package beater

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codingchipmunk/JolokiaGo"
	"github.com/codingchipmunk/JolokiaGo/jolokiaSSEStructs"
	"github.com/codingchipmunk/JolokiaGo/responseValues"
	"github.com/codingchipmunk/cameleventbeat/camelsse"
	"github.com/r3labs/sse"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"

	"github.com/codingchipmunk/cameleventbeat/config"
)

// Cameleventbeat configuration.
type Cameleventbeat struct {
	done    chan struct{}
	config  config.Config
	client  beat.Client
	streams eventstreams
}

//	eventstreams holds the streams used for communication between the goroutines
type eventstreams struct {
	//	sse_events holds the sse events received by the sse client
	sse_events chan *sse.Event
	//	beat_events holds the beat events which have been generated from the sse events and are ready to be transmitted
	beat_events chan *beat.Event
}

// New creates an instance of cameleventbeat.
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	c := config.DefaultConfig
	if err := cfg.Unpack(&c); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Cameleventbeat{
		done:   make(chan struct{}),
		config: c,
		streams: eventstreams{
			sse_events:  make(chan *sse.Event),
			beat_events: make(chan *beat.Event),
		},
	}
	return bt, nil
}

// Run starts cameleventbeat.
func (bt *Cameleventbeat) Run(b *beat.Beat) error {
	logp.Info("cameleventbeat is running! Hit CTRL-C to stop it.")

	var err error
	bt.client, err = b.Publisher.Connect()
	if err != nil {
		return err
	}

	// Retrieve a client id
	var client_id string
	client_id, err = get_SSE_id(bt.config.Jolokia.URL)
	if err != nil {
		return err
	}

	// Make a jolokiaClient MBean from the config MBean
	client_MBean := jolokiaClient.MBean{
		Domain:  bt.config.MBean.Domain,
		Name:    bt.config.MBean.Name,
		Context: bt.config.MBean.Context,
		Type:    bt.config.MBean.Type,
	}

	// Register for events
	err = register_MBean_for_SSE(bt.config.Jolokia.URL, client_id, client_MBean)
	if err != nil {
		return err
	}

	//create sse client
	client := sse.NewClient(bt.config.Jolokia.URL + "/notification/open/" + client_id + "/sse")
	//launch sse client in seperate goroutine
	go client.SubscribeChan("notification", bt.streams.sse_events)
	//bt.client.Publish(event)
	return nil
}

// Stop stops cameleventbeat.
func (bt *Cameleventbeat) Stop() {
	bt.client.Close()
	close(bt.done)
}

func publish_events(beat_events <-chan beat.Event, done <-chan interface{}, client beat.Client) {
	for{
		select {
		case <- done:
			return
		case event := <-beat_events:
			{
				client.Publish(event)
			}
		}
	}
}

func worker(sse_events <-chan *sse.Event, done <-chan interface{}, beat_events chan<- beat.Event, worker_id string) error {
	var event *sse.Event
	for {
		select {
		case <-done:
			return nil
		case event = <-sse_events:
			{
				beat_event, err := make_beat_event(event, worker_id)
				if err != nil {
					logp.Err("Worker %s encountered an error while trying to make a beat event: %s", worker_id, err)
				} else {
					beat_events <- beat_event
				}
			}

		}
	}

}

func unmarshall_sse_event(sse_event *sse.Event) (jolokiaSSEStructs.SSERoot, error) {
	var root jolokiaSSEStructs.SSERoot
	err := json.Unmarshal(sse_event.Data, &root)
	if err != nil {
		return root, err
	}

	return root, nil
}

func make_beat_event(sse_event *sse.Event, worker_id string) (beat.Event, error) {
	root, err := unmarshall_sse_event(sse_event)
	if err != nil {
		return beat.Event{}, err
	}

	data := camelsse.CamelSSEData{}
	err = json.Unmarshal(root.Notifications[0].UserData, data)
	if err != nil {
		return beat.Event{}, err
	}

	event := beat.Event{
		Timestamp: data.TimeStamp,
		Meta: common.MapStr{
			"worker_id":       worker_id,
			"jolokia_time":    root.Notifications[0].TimeStamp.Time,
			"event_processed": time.Now(),
		},
		Fields: common.MapStr{
			"endpoint_uri":      data.EndpointURI,
			"exchange_id":       data.ExchangeID,
			"breadcrumb":        data.Headers.Breadcrumbid,
			"body":              data.Body,
			"firedtime":         data.Headers.Firedtime.Time,
			"camel_to_endpoint": data.Properties.ToEndpoint,
			"camel_timer_name":  data.Properties.CamelTimer.Name,
		},
	}
	return event, nil
}

//	Register the client_ID at the jolokia agent for sse-events for the MBean
func register_MBean_for_SSE(agent_url string, client_ID string, bean jolokiaClient.MBean) error {
	var err error

	register_request := jolokiaClient.RegisterEventRequest{Mode: "sse", ClientID: client_ID, BaseRequest: jolokiaClient.BaseRequest{Type: "notification", Command: "add", MBean: &bean}}

	var resp_root jolokiaClient.ResponseRoot
	resp_root, err = make_Jolokia_Request(agent_url, register_request)
	if err != nil {
		return err
	}

	//Check if http-status is 200, throw an error if not
	if resp_root.Status != 200 {
		return errors.New("The http-request to the jolokia-Agent to register for MBean notifications was successfull, however the status-code embedded in the response is not 200 but " + string(resp_root.Status))
	}

	return nil
}


//	Requests the Jolokia agent on the given url to recieve a client id which is needed to register for sse requests
func get_SSE_id(agent_url string) (string, error) {
	var err error

	//Make a request to jolokia to get a client id
	var resp_root jolokiaClient.ResponseRoot
	resp_root, err = make_Jolokia_Request(agent_url, jolokiaClient.BaseRequest{Type: "notification", Command: "register"})
	if err != nil {
		return "", err
	}

	//Check if http-status is 200, throw an error if not
	if resp_root.Status != 200 {
		return "", errors.New("The http-request to the jolokia-Agent to retrieve a client-id was successfull, however the status-code embedded in the response is not 200 but " + string(resp_root.Status))
	}
	//Check if Value-Field is nil
	if resp_root.Value == nil {
		return "", errors.New("The http-request to the jolokia-Agent to retrieve a client-id was successfull, however there was no field labeled \"value\" in the response")
	}

	//Unmarshall Value-Field
	var sse_value responseValues.ResponseValue
	err = json.Unmarshal(resp_root.Value, &sse_value)
	if err != nil {
		return "", err
	}

	//Check if id field is empty
	if sse_value.Id == "" {
		return "", errors.New("The http-request to the jolokia-Agent to retrieve a client-id was successfull, however the id field was empty")
	}

	return sse_value.Id, nil
}


//	make_Jolokia_Request makes an request to the jolokia agent specified in agent_url.
//	The request is marshaled into JSON and transmitted to the jolokia agent in a POST request.
//	Afterwards the response is unmarshaled into a ResponseRoot struct and returned
func make_Jolokia_Request(agent_url string, request interface{}) (jolokiaClient.ResponseRoot, error) {
	var err error

	//Marshall the request
	var js []byte
	js, err = json.Marshal(request)
	if err != nil {
		return jolokiaClient.ResponseRoot{}, err
	}

	//Transmit the request to the jolokia Client
	var httpResp *http.Response
	httpResp, err = http.Post(agent_url, "application/json", bytes.NewReader(js))
	if err != nil {
		return jolokiaClient.ResponseRoot{}, err
	}

	//Close the response body so it can be read safely
	defer httpResp.Body.Close()
	//Read the response body
	var httpBody []byte
	httpBody, err = ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return jolokiaClient.ResponseRoot{}, err
	}

	//Unmarshall the response body into the ResponseRoot struct
	var jolokia_response jolokiaClient.ResponseRoot
	err = json.Unmarshal(httpBody, &jolokia_response)

	return jolokia_response, err

}
