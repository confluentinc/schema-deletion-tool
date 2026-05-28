package pkg

import (
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Platform interface {
	// ListSubjects returns all subjects from Schema Registry.
	// If strategy is "topic-name", only TopicNameStrategy subjects are returned.
	ListSubjects(strategy string) ([]string, error)

	// ListSchemas returns all schema versions for the given subjects.
	ListSchemas(subjects []string) ([]SchemaInfo, error)

	// DeleteSchema deletes a schema version. If permanent is true, hard-deletes it.
	DeleteSchema(subject string, version string, permanent bool) error

	// GetReferencedBy returns the list of schema IDs that reference this schema version.
	GetReferencedBy(subject string, version string) ([]int, error)

	// GetSchemaDetail returns full schema details including rules for a version.
	GetSchemaDetail(subject string, version string) (*SchemaDetail, error)

	// GetSubjectConfig returns compatibility and rule config for a subject.
	GetSubjectConfig(subject string) (*SubjectConfig, error)

	// GetGlobalConfig returns the global SR config (compatibility, default rules).
	GetGlobalConfig() (*GlobalConfig, error)

	// ListClusters returns available Kafka clusters.
	ListClusters() ([]ClusterInfo, error)

	// DescribeCluster returns bootstrap endpoint for a cluster.
	DescribeCluster(clusterID string) (string, error)

	// ListTopics returns topics available on a cluster.
	ListTopics(clusterID string) ([]string, error)

	// CreateConsumerConfig builds a Kafka consumer ConfigMap for the given cluster.
	CreateConsumerConfig(clusterID string, creds Credentials) (*kafka.ConfigMap, error)
}

type ClusterInfo struct {
	ID           string
	Name         string
	Type         string
	Provider     string
	Region       string
	Availability string
	Status       string
}

type Rule struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Type   string `json:"type"`
	Mode   string `json:"mode,omitempty"`
	Expr   string `json:"expr,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

type RuleSet struct {
	MigrationRules []Rule `json:"migrationRules,omitempty"`
	DomainRules    []Rule `json:"domainRules,omitempty"`
	EncodingRules  []Rule `json:"encodingRules,omitempty"`
}

type SchemaReference struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version int    `json:"version"`
}

type SchemaDetail struct {
	Subject    string            `json:"subject"`
	Version    int               `json:"version"`
	ID         int               `json:"id"`
	SchemaType string            `json:"schemaType"`
	Schema     string            `json:"schema"`
	References []SchemaReference `json:"references,omitempty"`
	RuleSet    *RuleSet          `json:"ruleSet,omitempty"`
}

type SubjectConfig struct {
	CompatibilityLevel string   `json:"compatibilityLevel,omitempty"`
	DefaultRuleSet     *RuleSet `json:"defaultRuleSet,omitempty"`
	OverrideRuleSet    *RuleSet `json:"overrideRuleSet,omitempty"`
}

type GlobalConfig struct {
	Compatibility  string   `json:"compatibility,omitempty"`
	DefaultRuleSet *RuleSet `json:"defaultRuleSet,omitempty"`
}

// CandidateStatus represents the safety status of a deletion candidate.
type CandidateStatus string

const (
	StatusSafe                    CandidateStatus = "safe"
	StatusBlockedByReferences     CandidateStatus = "blocked_by_references"
	StatusBlockedByMigrationChain CandidateStatus = "blocked_by_migration_chain"
	StatusBlockedByEncryption     CandidateStatus = "blocked_by_encryption_rules"
	StatusBlockedByRuleReference  CandidateStatus = "blocked_by_rule_reference"
	StatusWarnHasDomainRules      CandidateStatus = "has_domain_rules"
	StatusWarnHasMigrationRules   CandidateStatus = "has_migration_rules"
)

// DeletionCandidate is a schema version with safety analysis results.
type DeletionCandidate struct {
	Subject      string          `json:"subject"`
	Version      string          `json:"version"`
	SchemaID     string          `json:"schema_id"`
	Status       CandidateStatus `json:"status"`
	BlockReasons []string        `json:"block_reasons,omitempty"`
	Rules        *RuleSet        `json:"rules,omitempty"`
	ReferencedBy []int           `json:"referenced_by,omitempty"`
	InheritsGlobalRules bool     `json:"inherits_global_rules,omitempty"`
}

// IsBlocked returns true if this candidate should not be deleted.
func (c *DeletionCandidate) IsBlocked() bool {
	switch c.Status {
	case StatusBlockedByReferences, StatusBlockedByMigrationChain,
		StatusBlockedByEncryption, StatusBlockedByRuleReference:
		return true
	}
	return false
}

// IsWarning returns true if this candidate has warnings but can be deleted.
func (c *DeletionCandidate) IsWarning() bool {
	return c.Status == StatusWarnHasDomainRules || c.Status == StatusWarnHasMigrationRules
}

// Manifest is the output of a dry-run, used as input for --from-file.
type Manifest struct {
	ManifestVersion string              `json:"manifest_version"`
	GeneratedAt     string              `json:"generated_at"`
	Platform        string              `json:"platform"`
	Strategy        string              `json:"strategy"`
	SRUrl           string              `json:"sr_url,omitempty"`
	Environment     string              `json:"environment,omitempty"`
	Candidates      []DeletionCandidate `json:"candidates"`
	ScannedTopics   []string            `json:"scanned_topics,omitempty"`
	ScannedClusters []string            `json:"scanned_clusters,omitempty"`
	ActiveSchemaIDs []int32             `json:"active_schema_ids,omitempty"`
}

// CPClusterConfig holds configuration for a single CP Kafka cluster.
type CPClusterConfig struct {
	Name             string `json:"name"`
	BootstrapServers string `json:"bootstrap_servers"`
	SecurityProtocol string `json:"security_protocol,omitempty"`
	SASLMechanism    string `json:"sasl_mechanism,omitempty"`
	SASLUsername     string `json:"sasl_username,omitempty"`
	SASLPassword     string `json:"sasl_password,omitempty"`
	SSLCALocation    string `json:"ssl_ca_location,omitempty"`
	SSLCertLocation  string `json:"ssl_cert_location,omitempty"`
	SSLKeyLocation   string `json:"ssl_key_location,omitempty"`
}

// CPSRConfig holds Schema Registry connection config for CP.
type CPSRConfig struct {
	URL         string `json:"url"`
	Auth        string `json:"auth,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	BearerToken string `json:"bearer_token,omitempty"`
	SSLCALocation   string `json:"ssl_ca_location,omitempty"`
	SSLCertLocation string `json:"ssl_cert_location,omitempty"`
	SSLKeyLocation  string `json:"ssl_key_location,omitempty"`
}

// CPConfig is the full config file for Confluent Platform.
type CPConfig struct {
	SchemaRegistry CPSRConfig      `json:"schema_registry"`
	Clusters       []CPClusterConfig `json:"clusters"`
}
