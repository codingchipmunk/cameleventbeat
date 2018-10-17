package beater

import (
	"fmt"
	"github.com/r3labs/sse"
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

	ticker := time.NewTicker(bt.config.Period)
	counter := 1
	for {
		select {
		case <-bt.done:
			return nil
		case <-ticker.C:
		}

		event := beat.Event{
			Timestamp: time.Now(),
			Fields: common.MapStr{
				"type":    b.Info.Name,
				"counter": counter,
			},
		}
		bt.client.Publish(event)
		logp.Info("Event sent")
		counter++
	}
}

// Stop stops cameleventbeat.
func (bt *Cameleventbeat) Stop() {
	bt.client.Close()
	close(bt.done)
}
