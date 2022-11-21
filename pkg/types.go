package pkg

const (
	MagicByte     = 0
	MessageOffset = 5

	Confluent = "confluent"

	GREEN = "\033[32m"
	RED   = "\033[31m"
	RESET = "\033[0m"

	KEYONLY   = 1
	VALUEONLY = 2
	KEYVALUE  = 3
)

var (
	KafkaClusterFields = []interface{}{"ID", "Name", "Type", "Provider", "Region", "Availability", "Status"}
	SchemaInfoFields   = []interface{}{"SchemaID", "Subject", "Version"}
	TopicInfoFields    = []interface{}{"Topic", "ClusterID"}
)

type KafkaCluster struct {
	Availability string `json:"availability"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	Region       string `json:"region"`
	Status       string `json:"status"`
	Type         string `json:"type"`
}

type TopicName struct {
	Name string `json:"name"`
}

type TopicWithClusterInfo struct {
	Topic     string
	ClusterID string
}

type SchemaInfo struct {
	SchemaID string `json:"schema_id"`
	Subject  string `json:"subject"`
	Version  string `json:"version"`
}

type Credentials struct {
	ApiKey    string `json:"key"`
	ApiSecret string `json:"secret"`
}
