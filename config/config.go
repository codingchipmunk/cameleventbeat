// Config is put into a different package to prevent cyclic imports in case
// it is needed in several locations

package config

//	Config holds the configuration of the beat
type Config struct {
	//	Beat_name holds the prefered name of the beat used to diferentiate between multiple beats in a cluster environment.
	// Defaults to "cameleventbeat"
	Beat_name string  `config:"beat_name"`
	Jolokia   Jolokia `config:"jolokia"`
	Worker    Worker  `config:"worker"`
	MBean     MBean   `config:"mbean"`
}

//	Worker holds information to configure the workers
type Worker struct {
	//	Count specifies how much workers should be startet. The minimum value is 1.
	//	Defaults to 1
	Count  int    `config:"count" validate:"min=1"`
	//	Prefix specifies the prefix of the workers name, followed by its number.
	//	Defaults to "worker-"
	Prefix string `config:"prefix"`
}

//	Jolokia information like URL
type Jolokia struct {
	//	The base-url of the Jolokia-agent
	URL string `config:"url"`
}

//	MBean descripes the bean which should be listened to
type MBean struct {
	// Domain of the MBean. Has no default value and has to be specified in the config file
	Domain  string `config:"domain" validate:"required"`
	// Context of the MBean. Has no default value and has to be specified in the config file
	Context string `config:"context" validate:"required"`
	// Type of the MBean. Defaults to "tracer".
	Type    string `config:"type"`
	// Name of the MBean. Defaults to "Tracer".
	Name    string `config:"name"`
}

var DefaultConfig = Config{
	Beat_name: "cameleventbeat",
	Jolokia:   defaultJolokia,
	Worker:    defaultWorker,
	MBean:     defaultMBean,
}

var defaultWorker = Worker{
	Count:  1,
	Prefix: "worker-",
}

var defaultJolokia = Jolokia{
	URL: "http://localhost:8778/jolokia/",
}

var defaultMBean = MBean{
	Type: "tracer",
	Name: "Tracer",
}
