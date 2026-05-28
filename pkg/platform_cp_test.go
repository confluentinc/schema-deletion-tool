package pkg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCPConfig_Valid(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	config := CPConfig{
		SchemaRegistry: CPSRConfig{
			URL:      "http://localhost:8081",
			Auth:     "basic",
			Username: "admin",
			Password: "secret",
		},
		Clusters: []CPClusterConfig{
			{
				Name:             "prod-east",
				BootstrapServers: "broker1:9092",
				SecurityProtocol: "SASL_SSL",
				SASLMechanism:    "PLAIN",
				SASLUsername:      "user1",
				SASLPassword:     "pass1",
			},
			{
				Name:             "prod-west",
				BootstrapServers: "broker2:9092",
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(path, data, 0644)

	loaded, err := LoadCPConfig(path)
	req.NoError(err)
	req.Equal("http://localhost:8081", loaded.SchemaRegistry.URL)
	req.Equal("basic", loaded.SchemaRegistry.Auth)
	req.Len(loaded.Clusters, 2)
	req.Equal("prod-east", loaded.Clusters[0].Name)
	req.Equal("SASL_SSL", loaded.Clusters[0].SecurityProtocol)
}

func TestLoadCPConfig_MissingSRUrl(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	config := CPConfig{
		Clusters: []CPClusterConfig{
			{BootstrapServers: "broker:9092"},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(path, data, 0644)

	_, err := LoadCPConfig(path)
	req.Error(err)
	req.Contains(err.Error(), "schema_registry.url is required")
}

func TestLoadCPConfig_NoClusters(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	config := CPConfig{
		SchemaRegistry: CPSRConfig{URL: "http://localhost:8081"},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(path, data, 0644)

	_, err := LoadCPConfig(path)
	req.Error(err)
	req.Contains(err.Error(), "at least one cluster")
}

func TestLoadCPConfig_MissingBootstrapServers(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	config := CPConfig{
		SchemaRegistry: CPSRConfig{URL: "http://localhost:8081"},
		Clusters:       []CPClusterConfig{{Name: "test"}},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(path, data, 0644)

	_, err := LoadCPConfig(path)
	req.Error(err)
	req.Contains(err.Error(), "bootstrap_servers is required")
}

func TestLoadCPConfig_AutoName(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	config := CPConfig{
		SchemaRegistry: CPSRConfig{URL: "http://localhost:8081"},
		Clusters: []CPClusterConfig{
			{BootstrapServers: "broker:9092"},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(path, data, 0644)

	loaded, err := LoadCPConfig(path)
	req.NoError(err)
	req.Equal("cluster-0", loaded.Clusters[0].Name)
}

func TestLoadCPConfig_FileNotFound(t *testing.T) {
	req := require.New(t)
	_, err := LoadCPConfig("/nonexistent/config.json")
	req.Error(err)
}

func TestCPPlatform_ListSubjects(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/subjects" {
			json.NewEncoder(w).Encode([]string{"orders-value", "orders-key", "com.example.Order"})
		}
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	subjects, err := cp.ListSubjects("topic-name")
	req.NoError(err)
	req.Len(subjects, 2) // Only -value and -key

	subjects, err = cp.ListSubjects("record-name")
	req.NoError(err)
	req.Len(subjects, 3) // All
}

func TestCPPlatform_ListSchemas(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subjects/orders-value/versions":
			json.NewEncoder(w).Encode([]int{1, 2})
		case "/subjects/orders-value/versions/1":
			json.NewEncoder(w).Encode(map[string]interface{}{"subject": "orders-value", "version": 1, "id": 100})
		case "/subjects/orders-value/versions/2":
			json.NewEncoder(w).Encode(map[string]interface{}{"subject": "orders-value", "version": 2, "id": 101})
		}
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	schemas, err := cp.ListSchemas([]string{"orders-value"})
	req.NoError(err)
	req.Len(schemas, 2)
	req.Equal(json.Number("100"), schemas[0].SchemaID)
	req.Equal(json.Number("101"), schemas[1].SchemaID)
}

func TestCPPlatform_GetReferencedBy(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/subjects/orders-value/versions/1/referencedby" {
			json.NewEncoder(w).Encode([]int{200, 300})
		}
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	refs, err := cp.GetReferencedBy("orders-value", "1")
	req.NoError(err)
	req.Equal([]int{200, 300}, refs)
}

func TestCPPlatform_GetSchemaDetail(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/subjects/orders-value/versions/1" {
			detail := SchemaDetail{
				Subject:    "orders-value",
				Version:    1,
				ID:         100,
				SchemaType: "AVRO",
				RuleSet: &RuleSet{
					DomainRules: []Rule{{Name: "validateAge", Type: "CEL", Kind: "CONDITION"}},
				},
			}
			json.NewEncoder(w).Encode(detail)
		}
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	detail, err := cp.GetSchemaDetail("orders-value", "1")
	req.NoError(err)
	req.Equal("orders-value", detail.Subject)
	req.Len(detail.RuleSet.DomainRules, 1)
	req.Equal("validateAge", detail.RuleSet.DomainRules[0].Name)
}

func TestCPPlatform_BasicAuth(t *testing.T) {
	req := require.New(t)
	var receivedUser, receivedPass string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, receivedPass, _ = r.BasicAuth()
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "basic", Username: "admin", Password: "secret"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	_, err = cp.ListSubjects("topic-name")
	req.NoError(err)
	req.Equal("admin", receivedUser)
	req.Equal("secret", receivedPass)
}

func TestCPPlatform_BearerAuth(t *testing.T) {
	req := require.New(t)
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "bearer", BearerToken: "mytoken123"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	_, err = cp.ListSubjects("topic-name")
	req.NoError(err)
	req.Equal("Bearer mytoken123", receivedAuth)
}

func TestCPPlatform_NoAuth(t *testing.T) {
	req := require.New(t)
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	_, err = cp.ListSubjects("topic-name")
	req.NoError(err)
	req.Empty(receivedAuth)
}

func TestCPPlatform_DeleteSchema(t *testing.T) {
	req := require.New(t)
	var deletePath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deletePath = r.URL.RequestURI()
		w.WriteHeader(200)
		w.Write([]byte("1"))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	err = cp.DeleteSchema("orders-value", "1", false)
	req.NoError(err)
	req.Equal("/subjects/orders-value/versions/1", deletePath)

	err = cp.DeleteSchema("orders-value", "1", true)
	req.NoError(err)
	req.Equal("/subjects/orders-value/versions/1?permanent=true", deletePath)
}

func TestCPPlatform_ListClusters(t *testing.T) {
	req := require.New(t)

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: "http://localhost:8081", Auth: "none"},
		Clusters: []CPClusterConfig{
			{Name: "prod-east", BootstrapServers: "broker1:9092"},
			{Name: "prod-west", BootstrapServers: "broker2:9092"},
		},
	})
	req.NoError(err)

	clusters, err := cp.ListClusters()
	req.NoError(err)
	req.Len(clusters, 2)
	req.Equal("prod-east", clusters[0].ID)
	req.Equal("prod-west", clusters[1].ID)
	req.Equal("CP", clusters[0].Type)
}

func TestCPPlatform_DescribeCluster(t *testing.T) {
	req := require.New(t)

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: "http://localhost:8081", Auth: "none"},
		Clusters: []CPClusterConfig{
			{Name: "prod-east", BootstrapServers: "broker1:9092,broker2:9092"},
		},
	})
	req.NoError(err)

	endpoint, err := cp.DescribeCluster("prod-east")
	req.NoError(err)
	req.Equal("broker1:9092,broker2:9092", endpoint)

	_, err = cp.DescribeCluster("nonexistent")
	req.Error(err)
}

func TestCPPlatform_HTTPError(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error_code":401,"message":"Unauthorized"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "basic", Username: "wrong", Password: "wrong"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	_, err = cp.ListSubjects("topic-name")
	req.Error(err)
	req.Contains(err.Error(), "401")
}

func TestCPPlatform_ContextSubjectURLEncoding(t *testing.T) {
	req := require.New(t)
	// URL-encode the context subject for matching
	encoded := encodeSubject(":.myctx:orders-value")
	var receivedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, r.URL.RawPath)
		// r.URL.Path is auto-decoded; use RawPath for matching encoded paths
		rawPath := r.URL.RawPath
		if rawPath == "" {
			rawPath = r.URL.Path
		}
		switch {
		case rawPath == "/subjects/"+encoded+"/versions":
			json.NewEncoder(w).Encode([]int{1})
		case rawPath == "/subjects/"+encoded+"/versions/1":
			json.NewEncoder(w).Encode(map[string]interface{}{"subject": ":.myctx:orders-value", "version": 1, "id": 100})
		case rawPath == "/subjects/"+encoded+"/versions/1/referencedby":
			json.NewEncoder(w).Encode([]int{})
		case rawPath == "/config/"+encoded:
			json.NewEncoder(w).Encode(map[string]string{"compatibilityLevel": "BACKWARD"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// Test ListSchemas with context subject
	schemas, err := cp.ListSchemas([]string{":.myctx:orders-value"})
	req.NoError(err)
	req.Len(schemas, 1)
	req.Equal(":.myctx:orders-value", schemas[0].Subject)

	// Test GetReferencedBy with context subject
	refs, err := cp.GetReferencedBy(":.myctx:orders-value", "1")
	req.NoError(err)
	req.Empty(refs)

	// Test GetSubjectConfig with context subject
	config, err := cp.GetSubjectConfig(":.myctx:orders-value")
	req.NoError(err)
	req.Equal("BACKWARD", config.CompatibilityLevel)

	// Verify requests were handled (URL encoding worked)
	req.True(len(receivedPaths) >= 3, "Expected at least 3 requests, got %d", len(receivedPaths))
}

func TestEncodeSubject(t *testing.T) {
	req := require.New(t)
	req.Equal("orders-value", encodeSubject("orders-value"))
	req.Equal("%3A.myctx%3Aorders-value", encodeSubject(":.myctx:orders-value"))
	req.Equal("%3A.%3Aorders-value", encodeSubject(":.:orders-value"))
	req.Equal("com.example.Order", encodeSubject("com.example.Order"))
}

func TestCPPlatform_GetReferencedBy_404(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error_code":40403,"message":"Subject not found"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// 404 should return nil, nil (fail open)
	refs, err := cp.GetReferencedBy("orders-value", "1")
	req.NoError(err)
	req.Nil(refs)
}

func TestCPPlatform_GetReferencedBy_500(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error_code":500,"message":"Internal Server Error"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// 500 should return an error (not silently swallowed)
	_, err = cp.GetReferencedBy("orders-value", "1")
	req.Error(err)
	req.Contains(err.Error(), "500")
}

func TestCPPlatform_GetSubjectConfig_404(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error_code":40401,"message":"Subject not found"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// 404 means no subject-level config — return empty config, no error
	config, err := cp.GetSubjectConfig("orders-value")
	req.NoError(err)
	req.Empty(config.CompatibilityLevel)
}

func TestCPPlatform_GetSubjectConfig_500(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error_code":500,"message":"Internal Server Error"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// 500 should propagate as error
	_, err = cp.GetSubjectConfig("orders-value")
	req.Error(err)
	req.Contains(err.Error(), "500")
}

func TestCPPlatform_GetGlobalConfig_500(t *testing.T) {
	req := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error_code":500,"message":"Internal Server Error"}`))
	}))
	defer server.Close()

	cp, err := NewCPPlatform(CPConfig{
		SchemaRegistry: CPSRConfig{URL: server.URL, Auth: "none"},
		Clusters:       []CPClusterConfig{{Name: "test", BootstrapServers: "localhost:9092"}},
	})
	req.NoError(err)

	// 500 should propagate as error
	_, err = cp.GetGlobalConfig()
	req.Error(err)
	req.Contains(err.Error(), "500")
}

func TestBuildCPKafkaConfig_Plaintext(t *testing.T) {
	req := require.New(t)
	c := &CPClusterConfig{
		BootstrapServers: "broker:9092",
	}
	configMap, err := buildCPKafkaConfig(c)
	req.NoError(err)

	bs, _ := configMap.Get("bootstrap.servers", "")
	req.Equal("broker:9092", bs)

	protocol, _ := configMap.Get("security.protocol", "")
	req.Equal("PLAINTEXT", protocol)
}

func TestBuildCPKafkaConfig_SASL_SSL(t *testing.T) {
	req := require.New(t)
	c := &CPClusterConfig{
		BootstrapServers: "broker:9093",
		SecurityProtocol: "SASL_SSL",
		SASLMechanism:    "SCRAM-SHA-256",
		SASLUsername:      "user",
		SASLPassword:      "pass",
		SSLCALocation:    "/path/to/ca.pem",
	}
	configMap, err := buildCPKafkaConfig(c)
	req.NoError(err)

	protocol, _ := configMap.Get("security.protocol", "")
	req.Equal("SASL_SSL", protocol)

	mechanism, _ := configMap.Get("sasl.mechanism", "")
	req.Equal("SCRAM-SHA-256", mechanism)

	username, _ := configMap.Get("sasl.username", "")
	req.Equal("user", username)

	caLoc, _ := configMap.Get("ssl.ca.location", "")
	req.Equal("/path/to/ca.pem", caLoc)
}
