package beater

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codingchipmunk/JolokiaGo"
	"github.com/codingchipmunk/JolokiaGo/responseValues"
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
