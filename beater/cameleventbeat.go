package beater

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codingchipmunk/cameleventbeat/camelsse"
	"github.com/codingchipmunk/cameleventbeat/config"
	"github.com/codingchipmunk/jolokiago"
	"github.com/codingchipmunk/jolokiago/events"
	"github.com/codingchipmunk/jolokiago/java"
	"github.com/r3labs/sse"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
)

// Cameleventbeat configuration.
type Cameleventbeat struct {
	stop             chan interface{}
	config           config.Config
	client           beat.Client
	jolokia          jolokiago.Client
	beatEvents       chan beat.Event
	bean             java.MBean
	logger           *logp.Logger
	runningWorkers   *sync.WaitGroup
	publisherRunning *sync.WaitGroup
	listener         *events.Listener
}

// New creates an instance of cameleventbeat.
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	c := config.DefaultConfig
	if err := cfg.Unpack(&c); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Cameleventbeat{
		config:     c,
		beatEvents: make(chan beat.Event),
		jolokia:    jolokiago.New(c.Jolokia.URL, &http.Client{Timeout: c.Jolokia.Timeout}),
		bean: java.MBean{
			Domain:  c.MBean.Domain,
			Name:    c.MBean.Name,
			Context: c.MBean.Context,
			Type:    c.MBean.Type,
		},
		logger:           logp.NewLogger(b.Info.Name),
		runningWorkers:   &sync.WaitGroup{},
		publisherRunning: &sync.WaitGroup{},
		stop:             make(chan interface{}),
	}
	bt.logger.Info("Initialized")
	return bt, nil
}

// Run starts cameleventbeat.
func (bt *Cameleventbeat) Run(b *beat.Beat) (err error) {
	log := bt.logger
	log.Info("Starting...")

	//TODO Check if target mbean exists

	bt.client, err = b.Publisher.Connect()
	if err != nil {
		return err
	}

	// Retrieve a client id
	bt.listener, err = bt.jolokia.NewSSEListener(http.DefaultClient)
	if err != nil {
		return err
	}

	log.Infof("Got client id: %s", bt.listener.ClientID())
	rawEvents, started := bt.listener.StartListening()
	if !started {
		e := errors.New("listener did not start")
		log.Error(e.Error())
		return e
	}

	err = bt.listener.SubscribeToMBean(bt.bean)
	if err != nil {
		log.Errorf("Could not subscribe to MBean: %s", err)
		return err
	}
	bt.publisherRunning.Add(1)
	go publishEvents(bt.beatEvents, bt.client, bt.publisherRunning)
	log.Debugf("Listening to URL: %s", bt.listener.FullURL())
	log.Infof("Starting %d workers...", bt.config.Worker.Count)
	for i := 1; i <= bt.config.Worker.Count; i++ {
		bt.runningWorkers.Add(1)
		go worker(rawEvents, bt.beatEvents, fmt.Sprintf("%s.%s%d", b.Info.Name, bt.config.Worker.Prefix, i), bt.runningWorkers)
	}
	log.Info("All workers running")
	// Register for events

	log.Info("Up and running! Hit CTRL-C (SIGINT) to gracefully stop.")
	<-bt.stop

	bt.listener.Stop()
	bt.logger.Info("Listening stopped")
	bt.logger.Info("Waiting for backlog being processed...")
	bt.runningWorkers.Wait()
	bt.logger.Info("All workers exited")
	close(bt.beatEvents)
	bt.logger.Info("Waiting for last events being published...")
	bt.publisherRunning.Wait()
	bt.logger.Info("All events published. Exiting now.")
	close(bt.stop)
	return nil
}

// Stop stops cameleventbeat.
func (bt *Cameleventbeat) Stop() {
	bt.logger.Info("SIGINT received, stopping...")
	bt.stop <- true
	<-bt.stop
	bt.client.Close()
}

func publishEvents(beatEvents <-chan beat.Event, client beat.Client, running *sync.WaitGroup) {
	defer running.Done()
	for {
		event, ok := <-beatEvents
		if ok {
			client.Publish(event)
		} else {
			return
		}
	}
}

func worker(sseEvents <-chan *sse.Event, beatEvents chan<- beat.Event, workerID string, running *sync.WaitGroup) {
	defer running.Done()
	log := logp.NewLogger(workerID)
	log.Debugf("Running and waiting for work...")

	for {
		event, ok := <-sseEvents
		if ok {
			beatEvent, err := makeBeatEvent(event, workerID)
			if err != nil {
				log.Errorf("Encountered an error while trying to make a beat event: %s", err)
				log.Debug(event)
			} else {
				beatEvents <- beatEvent
			}
		} else {
			break
		}
	}
	log.Debugf("Channel closed, exiting...")
}

func unmarshalSSEEvent(sseEvent *sse.Event) (events.EventData, error) {
	var root events.EventData
	err := json.Unmarshal(sseEvent.Data, &root)
	if err != nil {
		return root, err
	}

	return root, nil
}

func makeBeatEvent(sseEvent *sse.Event, workerID string) (beat.Event, error) {
	root, err := unmarshalSSEEvent(sseEvent)
	if err != nil {
		return beat.Event{}, err
	}

	var data camelsse.CamelSSEData
	err = json.Unmarshal(root.Notifications[0].UserData, &data)
	if err != nil {
		return beat.Event{}, err
	}

	event := beat.Event{
		Timestamp: data.TimeStamp,
		Fields: common.MapStr{
			"beat": common.MapStr{
				"workerid":        workerID,
				"event_processed": time.Now().UnixNano(),
			},
		},
	}
	cmlMap, e := getCamelMap(data)
	if e {
		event.Fields.Put("camel", cmlMap)
	}
	return event, nil
}

func getCamelMap(data camelsse.CamelSSEData) (mp common.MapStr, notEmpty bool) {
	mp = map[string]interface{}{}
	if data.EndpointURI != "" {
		mp.Put("endpoint_uri", data.EndpointURI)
		notEmpty = true
	}
	if data.ExchangeID != "" {
		mp.Put("exchange_id", data.ExchangeID)
		notEmpty = true
	}
	if !data.Headers.Firedtime.Time.IsZero() {
		mp.Put("firedtime", data.Headers.Firedtime.Time.UnixNano())
		notEmpty = true
	}

	if data.Properties.ToEndpoint != "" {
		mp.Put("to_endpoint", data.Properties.ToEndpoint)
		notEmpty = true
	}
	eventMP, e := getEventMap(data)
	if e {
		mp.Put("event", eventMP)
		notEmpty = true
	}
	timrMP, e := getTimerMap(data)
	if e {
		mp.Put("timer", timrMP)
		notEmpty = true
	}
	return
}
func getEventMap(data camelsse.CamelSSEData) (mp common.MapStr, empty bool) {
	mp = map[string]interface{}{}
	if data.Headers.Breadcrumbid != "" {
		mp.Put("breadcrumb", data.Headers.Breadcrumbid)
		empty = true
	}
	if data.Body != "" {
		mp.Put("body", data.Body)
		empty = true
	}
	return
}

func clean(mappings common.MapStr) (mp common.MapStr, notEmpty bool) {
	for k, v := range (mappings) {
		if v != "" {
			mp.Put(k, v)
			notEmpty = true
		}
	}
	return
}

func getTimerMap(data camelsse.CamelSSEData) (mp common.MapStr, notEmpty bool) {
	mp = map[string]interface{}{}
	tmr := data.Properties.CamelTimer
	if tmr.Name != "" {
		mp.Put("name", tmr.Name)
		notEmpty = true
	}

	if tmr.Counter != "" {
		number, _ := strconv.Atoi(tmr.Counter)
		mp.Put("counter", number)
		notEmpty = true
	}

	if tmr.Period != "" {
		number, _ := strconv.Atoi(tmr.Period)
		mp.Put("period", number)
		notEmpty = true
	}

	if !tmr.FiredTime.Time.IsZero() {
		mp.Put("firedtime", tmr.FiredTime.Time.UnixNano())
		notEmpty = true
	}

	return
}
