package pkg

import (
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// MockPlatform implements Platform for testing.
type MockPlatform struct {
	Subjects       []string
	Schemas        map[string][]SchemaInfo       // subject -> schemas
	SchemaDetails  map[string]*SchemaDetail       // "subject:version" -> detail
	References     map[string][]int               // "subject:version" -> referenced by IDs
	SubjectConfigs map[string]*SubjectConfig       // subject -> config
	GlobalCfg      *GlobalConfig
	Clusters       []ClusterInfo
	ClusterTopics  map[string][]string             // clusterID -> topics
	Endpoints      map[string]string               // clusterID -> endpoint
}

func NewMockPlatform() *MockPlatform {
	return &MockPlatform{
		Schemas:        make(map[string][]SchemaInfo),
		SchemaDetails:  make(map[string]*SchemaDetail),
		References:     make(map[string][]int),
		SubjectConfigs: make(map[string]*SubjectConfig),
		GlobalCfg:      &GlobalConfig{},
		ClusterTopics:  make(map[string][]string),
		Endpoints:      make(map[string]string),
	}
}

func (m *MockPlatform) ListSubjects(strategy string) ([]string, error) {
	var result []string
	for _, s := range m.Subjects {
		if VerifySubjectForStrategy(s, strategy) {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *MockPlatform) ListSchemas(subjects []string) ([]SchemaInfo, error) {
	var result []SchemaInfo
	for _, s := range subjects {
		result = append(result, m.Schemas[s]...)
	}
	return result, nil
}

func (m *MockPlatform) DeleteSchema(subject string, version string, permanent bool) error {
	return nil
}

func (m *MockPlatform) GetReferencedBy(subject string, version string) ([]int, error) {
	key := subject + ":" + version
	return m.References[key], nil
}

func (m *MockPlatform) GetSchemaDetail(subject string, version string) (*SchemaDetail, error) {
	key := subject + ":" + version
	detail, ok := m.SchemaDetails[key]
	if !ok {
		return &SchemaDetail{}, nil
	}
	return detail, nil
}

func (m *MockPlatform) GetSubjectConfig(subject string) (*SubjectConfig, error) {
	config, ok := m.SubjectConfigs[subject]
	if !ok {
		return &SubjectConfig{}, nil
	}
	return config, nil
}

func (m *MockPlatform) GetGlobalConfig() (*GlobalConfig, error) {
	return m.GlobalCfg, nil
}

func (m *MockPlatform) ListClusters() ([]ClusterInfo, error) {
	return m.Clusters, nil
}

func (m *MockPlatform) DescribeCluster(clusterID string) (string, error) {
	endpoint, ok := m.Endpoints[clusterID]
	if !ok {
		return "", fmt.Errorf("cluster not found: %s", clusterID)
	}
	return endpoint, nil
}

func (m *MockPlatform) ListTopics(clusterID string) ([]string, error) {
	return m.ClusterTopics[clusterID], nil
}

func (m *MockPlatform) CreateConsumerConfig(clusterID string, creds Credentials) (*kafka.ConfigMap, error) {
	return &kafka.ConfigMap{}, nil
}
