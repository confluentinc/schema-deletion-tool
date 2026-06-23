package pkg

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// srHTTPError is returned by doRequest for HTTP error responses,
// allowing callers to distinguish 404 (expected) from real errors.
type srHTTPError struct {
	StatusCode int
	Body       string
}

func (e *srHTTPError) Error() string {
	return fmt.Sprintf("SR returned HTTP %d: %s", e.StatusCode, e.Body)
}

// encodeSubject URL-encodes a subject name for use in REST API paths.
// Handles context-qualified subjects like ":.mycontext:orders-value".
// Uses url.QueryEscape which encodes colons, dots, etc. then replaces + with %20.
func encodeSubject(subject string) string {
	return strings.ReplaceAll(url.QueryEscape(subject), "+", "%20")
}

// CPPlatform implements Platform for Confluent Platform using direct REST API.
type CPPlatform struct {
	SRConfig CPSRConfig
	Clusters []CPClusterConfig
	client   *http.Client
}

func NewCPPlatform(config CPConfig) (*CPPlatform, error) {
	client, err := buildHTTPClient(config.SchemaRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client for Schema Registry: %w", err)
	}
	return &CPPlatform{
		SRConfig: config.SchemaRegistry,
		Clusters: config.Clusters,
		client:   client,
	}, nil
}

func buildHTTPClient(srConfig CPSRConfig) (*http.Client, error) {
	tlsConfig := &tls.Config{}
	needsCustomTLS := false

	if srConfig.SSLCALocation != "" {
		caCert, err := os.ReadFile(srConfig.SSLCALocation)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConfig.RootCAs = caCertPool
		needsCustomTLS = true
	}

	if srConfig.SSLCertLocation != "" && srConfig.SSLKeyLocation != "" {
		cert, err := tls.LoadX509KeyPair(srConfig.SSLCertLocation, srConfig.SSLKeyLocation)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		needsCustomTLS = true
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if needsCustomTLS {
		transport.TLSClientConfig = tlsConfig
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

func (cp *CPPlatform) doRequest(method, path string) ([]byte, error) {
	url := strings.TrimRight(cp.SRConfig.URL, "/") + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.schemaregistry.v1+json")

	switch cp.SRConfig.Auth {
	case "basic":
		req.SetBasicAuth(cp.SRConfig.Username, cp.SRConfig.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+cp.SRConfig.BearerToken)
	case "none", "":
		// No auth
	}

	resp, err := cp.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SR request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, &srHTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

func (cp *CPPlatform) ListSubjects(strategy string) ([]string, error) {
	body, err := cp.doRequest("GET", "/subjects")
	if err != nil {
		return nil, err
	}
	var allSubjects []string
	if err = json.Unmarshal(body, &allSubjects); err != nil {
		return nil, err
	}
	var result []string
	for _, s := range allSubjects {
		if VerifySubjectForStrategy(s, strategy) {
			result = append(result, s)
		}
	}
	return result, nil
}

func (cp *CPPlatform) ListSchemas(subjects []string) ([]SchemaInfo, error) {
	var schemas []SchemaInfo
	for _, subject := range subjects {
		fmt.Printf("Scanning schemas under subject %s...", subject)
		versionsBody, err := cp.doRequest("GET", fmt.Sprintf("/subjects/%s/versions", encodeSubject(subject)))
		if err != nil {
			return nil, err
		}
		var versions []int
		if err = json.Unmarshal(versionsBody, &versions); err != nil {
			return nil, err
		}
		for _, v := range versions {
			detailBody, err := cp.doRequest("GET", fmt.Sprintf("/subjects/%s/versions/%d", encodeSubject(subject), v))
			if err != nil {
				return nil, err
			}
			var detail struct {
				Subject  string `json:"subject"`
				Version  int    `json:"version"`
				SchemaID int    `json:"id"`
			}
			if err = json.Unmarshal(detailBody, &detail); err != nil {
				return nil, err
			}
			schemas = append(schemas, SchemaInfo{
				Subject:  detail.Subject,
				Version:  json.Number(fmt.Sprintf("%d", detail.Version)),
				SchemaID: json.Number(fmt.Sprintf("%d", detail.SchemaID)),
			})
		}
		fmt.Printf("  Found %d schema(s).\n", len(versions))
	}
	return schemas, nil
}

func (cp *CPPlatform) DeleteSchema(subject string, version string, permanent bool) error {
	path := fmt.Sprintf("/subjects/%s/versions/%s", encodeSubject(subject), version)
	if permanent {
		path += "?permanent=true"
	}
	_, err := cp.doRequest("DELETE", path)
	return err
}

func (cp *CPPlatform) GetReferencedBy(subject string, version string) ([]int, error) {
	body, err := cp.doRequest("GET", fmt.Sprintf("/subjects/%s/versions/%s/referencedby", encodeSubject(subject), version))
	if err != nil {
		var httpErr *srHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			// Endpoint not available (older SR versions), fail open
			return nil, nil
		}
		return nil, fmt.Errorf("referencedby check failed for %s:%s: %w", subject, version, err)
	}
	var refs []int
	if err = json.Unmarshal(body, &refs); err != nil {
		return nil, fmt.Errorf("failed to parse referencedby response for %s:%s: %w", subject, version, err)
	}
	return refs, nil
}

func (cp *CPPlatform) GetSchemaDetail(subject string, version string) (*SchemaDetail, error) {
	body, err := cp.doRequest("GET", fmt.Sprintf("/subjects/%s/versions/%s", encodeSubject(subject), version))
	if err != nil {
		return nil, err
	}
	var detail SchemaDetail
	if err = json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (cp *CPPlatform) GetSubjectConfig(subject string) (*SubjectConfig, error) {
	body, err := cp.doRequest("GET", fmt.Sprintf("/config/%s", encodeSubject(subject)))
	if err != nil {
		var httpErr *srHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			// No subject-level config set, will inherit global
			return &SubjectConfig{}, nil
		}
		return nil, fmt.Errorf("failed to get config for subject %s: %w", subject, err)
	}
	var config SubjectConfig
	if err = json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config for subject %s: %w", subject, err)
	}
	return &config, nil
}

func (cp *CPPlatform) GetGlobalConfig() (*GlobalConfig, error) {
	body, err := cp.doRequest("GET", "/config")
	if err != nil {
		var httpErr *srHTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			return &GlobalConfig{}, nil
		}
		return nil, fmt.Errorf("failed to get global config: %w", err)
	}
	var config GlobalConfig
	if err = json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to parse global config: %w", err)
	}
	return &config, nil
}

func (cp *CPPlatform) ListClusters() ([]ClusterInfo, error) {
	var result []ClusterInfo
	for i, c := range cp.Clusters {
		name := c.Name
		if name == "" {
			name = fmt.Sprintf("cluster-%d", i)
		}
		result = append(result, ClusterInfo{
			ID:   name,
			Name: name,
			Type: "CP",
		})
	}
	return result, nil
}

func (cp *CPPlatform) DescribeCluster(clusterID string) (string, error) {
	for _, c := range cp.Clusters {
		if c.Name == clusterID {
			return c.BootstrapServers, nil
		}
	}
	return "", fmt.Errorf("cluster %s not found in config", clusterID)
}

func (cp *CPPlatform) ListTopics(clusterID string) ([]string, error) {
	var clusterConfig *CPClusterConfig
	for i := range cp.Clusters {
		if cp.Clusters[i].Name == clusterID {
			clusterConfig = &cp.Clusters[i]
			break
		}
	}
	if clusterConfig == nil {
		return nil, fmt.Errorf("cluster %s not found in config", clusterID)
	}

	configMap, err := buildCPKafkaConfig(clusterConfig)
	if err != nil {
		return nil, err
	}

	admin, err := kafka.NewAdminClient(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin client for %s: %w", clusterID, err)
	}
	defer admin.Close()

	metadata, err := admin.GetMetadata(nil, true, 10000)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata from %s: %w", clusterID, err)
	}

	var topics []string
	for topic := range metadata.Topics {
		if !strings.HasPrefix(topic, "_") {
			topics = append(topics, topic)
		}
	}
	return topics, nil
}

func (cp *CPPlatform) CreateConsumerConfig(clusterID string, creds Credentials) (*kafka.ConfigMap, error) {
	var clusterConfig *CPClusterConfig
	for i := range cp.Clusters {
		if cp.Clusters[i].Name == clusterID {
			clusterConfig = &cp.Clusters[i]
			break
		}
	}
	if clusterConfig == nil {
		return nil, fmt.Errorf("cluster %s not found in config", clusterID)
	}

	configMap, err := buildCPKafkaConfig(clusterConfig)
	if err != nil {
		return nil, err
	}
	if err = configMap.SetKey("group.id", fmt.Sprintf("schema-deletion-tool-%s-%d", clusterID, time.Now().UnixNano())); err != nil {
		return nil, err
	}
	if err = configMap.SetKey("enable.auto.commit", false); err != nil {
		return nil, err
	}
	if err = configMap.SetKey("auto.offset.reset", "earliest"); err != nil {
		return nil, err
	}
	if err = configMap.SetKey("enable.partition.eof", true); err != nil {
		return nil, err
	}
	return configMap, nil
}

func buildCPKafkaConfig(c *CPClusterConfig) (*kafka.ConfigMap, error) {
	configMap := &kafka.ConfigMap{}
	if err := configMap.SetKey("bootstrap.servers", c.BootstrapServers); err != nil {
		return nil, err
	}

	protocol := c.SecurityProtocol
	if protocol == "" {
		protocol = "PLAINTEXT"
	}
	if err := configMap.SetKey("security.protocol", protocol); err != nil {
		return nil, err
	}

	if strings.Contains(strings.ToUpper(protocol), "SASL") {
		mechanism := c.SASLMechanism
		if mechanism == "" {
			mechanism = "PLAIN"
		}
		if err := configMap.SetKey("sasl.mechanism", mechanism); err != nil {
			return nil, err
		}
		if err := configMap.SetKey("sasl.username", c.SASLUsername); err != nil {
			return nil, err
		}
		if err := configMap.SetKey("sasl.password", c.SASLPassword); err != nil {
			return nil, err
		}
	}

	if strings.Contains(strings.ToUpper(protocol), "SSL") {
		if c.SSLCALocation != "" {
			if err := configMap.SetKey("ssl.ca.location", c.SSLCALocation); err != nil {
				return nil, err
			}
		}
		if c.SSLCertLocation != "" {
			if err := configMap.SetKey("ssl.certificate.location", c.SSLCertLocation); err != nil {
				return nil, err
			}
		}
		if c.SSLKeyLocation != "" {
			if err := configMap.SetKey("ssl.key.location", c.SSLKeyLocation); err != nil {
				return nil, err
			}
		}
	}

	return configMap, nil
}

// LoadCPConfig reads and parses a CP config file.
func LoadCPConfig(path string) (*CPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CP config file: %w", err)
	}
	var config CPConfig
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse CP config file: %w", err)
	}
	if config.SchemaRegistry.URL == "" {
		return nil, fmt.Errorf("schema_registry.url is required in CP config file")
	}
	if len(config.Clusters) == 0 {
		return nil, fmt.Errorf("at least one cluster is required in CP config file")
	}
	for i := range config.Clusters {
		if config.Clusters[i].BootstrapServers == "" {
			return nil, fmt.Errorf("bootstrap_servers is required for cluster %d", i)
		}
		if config.Clusters[i].Name == "" {
			config.Clusters[i].Name = fmt.Sprintf("cluster-%d", i)
		}
	}
	return &config, nil
}
