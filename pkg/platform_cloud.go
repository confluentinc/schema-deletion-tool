package pkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// CloudPlatform implements Platform for Confluent Cloud using the `confluent` CLI.
// For operations not exposed by the CLI (referencedby), it uses the SR REST API
// directly if SR credentials are provided.
type CloudPlatform struct {
	srURL       string
	srAPIKey    string
	srAPISecret string
	httpClient  *http.Client
}

func NewCloudPlatform() *CloudPlatform {
	return &CloudPlatform{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetSRCredentials configures direct SR REST API access for operations
// not available via the confluent CLI (e.g., referencedby).
func (c *CloudPlatform) SetSRCredentials(srURL, apiKey, apiSecret string) {
	c.srURL = srURL
	c.srAPIKey = apiKey
	c.srAPISecret = apiSecret
}

func (c *CloudPlatform) ListSubjects(strategy string) ([]string, error) {
	output, err := ExecuteCommand(Confluent, []string{"schema-registry", "subject", "list", "-o", "json"}, false)
	if err != nil {
		return nil, errors.New(`error while listing Schema Registry subjects. Check you have proper credentials` +
			` stored by running any Schema Registry command, e.g. "confluent schema-registry subject list"`)
	}
	var subjects []map[string]string
	if err = json.Unmarshal(output, &subjects); err != nil {
		return nil, err
	}
	var result []string
	for _, s := range subjects {
		if VerifySubjectForStrategy(s["subject"], strategy) {
			result = append(result, s["subject"])
		}
	}
	return result, nil
}

func (c *CloudPlatform) ListSchemas(subjects []string) ([]SchemaInfo, error) {
	var schemas []SchemaInfo
	for _, subject := range subjects {
		fmt.Printf("Scanning schemas under subject %s...", subject)
		output, err := ExecuteCommand(Confluent, []string{"schema-registry", "schema", "list", "--subject-prefix", subject, "-o", "json"}, false)
		if err != nil {
			return nil, errors.New(`error while listing all schemas. Check you have proper credentials` +
				` stored by running any Schema Registry command, e.g. "confluent schema-registry subject list"`)
		}
		var subjectSchemas []SchemaInfo
		if err = json.Unmarshal(output, &subjectSchemas); err != nil {
			return nil, err
		}
		fmt.Printf("  Found %d schema(s).\n", len(subjectSchemas))
		schemas = append(schemas, subjectSchemas...)
	}
	return schemas, nil
}

func (c *CloudPlatform) DeleteSchema(subject string, version string, permanent bool) error {
	args := []string{"schema-registry", "schema", "delete", "--subject", subject, "--version", version, "--force"}
	if permanent {
		args = append(args, "--permanent")
	}
	_, err := ExecuteCommand(Confluent, args, true)
	return err
}

func (c *CloudPlatform) GetReferencedBy(subject string, version string) ([]int, error) {
	// The confluent CLI doesn't expose referencedby. Use SR REST API if credentials are configured.
	if c.srURL == "" || c.srAPIKey == "" {
		// No SR credentials — cannot check references. Return nil (skip check).
		return nil, nil
	}

	encodedSubject := encodeSubject(subject)
	reqURL := fmt.Sprintf("%s/subjects/%s/versions/%s/referencedby", c.srURL, encodedSubject, version)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.srAPIKey, c.srAPISecret)
	req.Header.Set("Accept", "application/vnd.schemaregistry.v1+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("referencedby request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		if resp.StatusCode == 404 {
			return nil, nil
		}
		return nil, fmt.Errorf("referencedby returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var refs []int
	if err = json.Unmarshal(body, &refs); err != nil {
		return nil, fmt.Errorf("failed to parse referencedby response: %w", err)
	}
	return refs, nil
}

func (c *CloudPlatform) GetSchemaDetail(subject string, version string) (*SchemaDetail, error) {
	output, err := ExecuteCommand(Confluent, []string{
		"schema-registry", "schema", "describe",
		"--subject", subject, "--version", version, "-o", "json",
	}, true)
	if err != nil {
		return nil, err
	}
	var detail SchemaDetail
	if err = json.Unmarshal(output, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (c *CloudPlatform) GetSubjectConfig(subject string) (*SubjectConfig, error) {
	output, err := ExecuteCommand(Confluent, []string{
		"schema-registry", "compatibility", "describe",
		"--subject", subject, "-o", "json",
	}, true)
	if err != nil {
		// CLI exits non-zero both for "no config set" (common) and real errors.
		// We can't distinguish them without parsing stderr, so we fail open.
		return &SubjectConfig{}, nil
	}
	var config SubjectConfig
	if err = json.Unmarshal(output, &config); err != nil {
		return &SubjectConfig{}, nil
	}
	return &config, nil
}

func (c *CloudPlatform) GetGlobalConfig() (*GlobalConfig, error) {
	output, err := ExecuteCommand(Confluent, []string{
		"schema-registry", "compatibility", "describe", "-o", "json",
	}, true)
	if err != nil {
		// CLI exits non-zero both for "no config set" and real errors.
		// We can't distinguish them without parsing stderr, so we fail open.
		return &GlobalConfig{}, nil
	}
	var config GlobalConfig
	if err = json.Unmarshal(output, &config); err != nil {
		return &GlobalConfig{}, nil
	}
	return &config, nil
}

func (c *CloudPlatform) ListClusters() ([]ClusterInfo, error) {
	output, err := ExecuteCommand(Confluent, []string{"kafka", "cluster", "list", "-o", "json"}, false)
	if err != nil {
		return nil, err
	}
	var clusters []KafkaCluster
	if err = json.Unmarshal(output, &clusters); err != nil {
		return nil, err
	}
	var result []ClusterInfo
	for _, kc := range clusters {
		result = append(result, ClusterInfo{
			ID:           kc.ID,
			Name:         kc.Name,
			Type:         kc.Type,
			Provider:     kc.Provider,
			Region:       kc.Region,
			Availability: kc.Availability,
			Status:       kc.Status,
		})
	}
	return result, nil
}

func (c *CloudPlatform) DescribeCluster(clusterID string) (string, error) {
	output, err := ExecuteCommand(Confluent, []string{"kafka", "cluster", "describe", clusterID, "-o", "json"}, false)
	if err != nil {
		return "", err
	}
	var cluster map[string]interface{}
	if err = json.Unmarshal(output, &cluster); err != nil {
		return "", err
	}
	endpoint, ok := cluster["endpoint"].(string)
	if !ok {
		return "", errors.New("cluster endpoint not found")
	}
	return endpoint, nil
}

func (c *CloudPlatform) ListTopics(clusterID string) ([]string, error) {
	output, err := ExecuteCommand(Confluent, []string{"kafka", "topic", "list", "--cluster", clusterID, "-o", "json"}, false)
	if err != nil {
		return nil, err
	}
	var topics []TopicName
	if err = json.Unmarshal(output, &topics); err != nil {
		return nil, err
	}
	var result []string
	for _, t := range topics {
		result = append(result, t.Name)
	}
	return result, nil
}

func (c *CloudPlatform) CreateConsumerConfig(clusterID string, creds Credentials) (*kafka.ConfigMap, error) {
	ccfg := &kafka.ConfigMap{}
	if err := ccfg.SetKey("sasl.mechanism", "PLAIN"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("security.protocol", "SASL_SSL"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("ssl.endpoint.identification.algorithm", "https"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("sasl.username", creds.ApiKey); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("sasl.password", creds.ApiSecret); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("group.id", fmt.Sprintf("schema-deletion-tool-%s-%d", clusterID, time.Now().UnixNano())); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("enable.auto.commit", false); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("auto.offset.reset", "earliest"); err != nil {
		return nil, err
	}
	return ccfg, nil
}

