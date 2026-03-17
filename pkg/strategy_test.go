package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifySubjectForStrategy_TopicName(t *testing.T) {
	req := require.New(t)
	req.True(VerifySubjectForStrategy("orders-value", "topic-name"))
	req.True(VerifySubjectForStrategy("orders-key", "topic-name"))
	req.False(VerifySubjectForStrategy("com.example.Order", "topic-name"))
	req.False(VerifySubjectForStrategy("orders", "topic-name"))
}

func TestVerifySubjectForStrategy_RecordName(t *testing.T) {
	req := require.New(t)
	req.True(VerifySubjectForStrategy("com.example.Order", "record-name"))
	req.True(VerifySubjectForStrategy("orders-value", "record-name"))
	req.True(VerifySubjectForStrategy("anything", "record-name"))
	req.False(VerifySubjectForStrategy("", "record-name"))
}

func TestVerifySubjectForStrategy_TopicRecordName(t *testing.T) {
	req := require.New(t)
	req.True(VerifySubjectForStrategy("orders-com.example.Order", "topic-record-name"))
	req.True(VerifySubjectForStrategy("my-topic-com.example.Foo", "topic-record-name"))
	req.False(VerifySubjectForStrategy("orders-value", "topic-record-name"))
	req.False(VerifySubjectForStrategy("orders", "topic-record-name"))
	// False positive from old check: topic with dot should not match
	req.False(VerifySubjectForStrategy("my.topic-value", "topic-record-name"))
	// No hyphen at all
	req.False(VerifySubjectForStrategy("com.example.Order", "topic-record-name"))
	// With context prefix
	req.True(VerifySubjectForStrategy(":.staging:orders-com.example.Order", "topic-record-name"))
	req.False(VerifySubjectForStrategy(":.staging:orders-value", "topic-record-name"))
}

func TestVerifySubjectForStrategy_WithContext(t *testing.T) {
	req := require.New(t)
	// Context-prefixed subjects should work with all strategies
	req.True(VerifySubjectForStrategy(":.ctx:orders-value", "topic-name"))
	req.True(VerifySubjectForStrategy(":.ctx:com.example.Order", "record-name"))
	req.False(VerifySubjectForStrategy(":.ctx:orders", "topic-name"))
}

func TestResolveTopics_TopicName(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"orders", "payments", "inventory"}

	subjects := []string{"orders-value", "orders-key"}
	result, err := ResolveTopics(subjects, "topic-name", nil, false, mock, []string{"cluster-1"})
	req.NoError(err)
	req.Len(result, 1) // orders topic on cluster-1
	req.Equal("orders", result[0].Topic)
}

func TestResolveTopics_ExplicitTopics(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"orders", "payments"}
	mock.ClusterTopics["cluster-2"] = []string{"orders", "inventory"}

	result, err := ResolveTopics(nil, "record-name", []string{"orders", "payments"}, false, mock, []string{"cluster-1", "cluster-2"})
	req.NoError(err)
	req.Len(result, 3) // orders on c1, payments on c1, orders on c2
}

func TestResolveTopics_ScanAllTopics(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"orders", "payments"}
	mock.ClusterTopics["cluster-2"] = []string{"inventory"}

	result, err := ResolveTopics(nil, "record-name", nil, true, mock, []string{"cluster-1", "cluster-2"})
	req.NoError(err)
	req.Len(result, 3)
}

func TestResolveTopics_RecordNameRequiresTopics(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	_, err := ResolveTopics([]string{"com.example.Order"}, "record-name", nil, false, mock, []string{"cluster-1"})
	req.Error(err)
	req.Contains(err.Error(), "--topics or --scan-all-topics")
}

func TestResolveTopics_TopicRecordName(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"orders", "my-topic", "my"}

	subjects := []string{"orders-com.example.Order", "my-topic-com.example.Foo"}
	result, err := ResolveTopics(subjects, "topic-record-name", nil, false, mock, []string{"cluster-1"})
	req.NoError(err)
	req.Len(result, 2)

	topicNames := make(map[string]bool)
	for _, r := range result {
		topicNames[r.Topic] = true
	}
	req.True(topicNames["orders"])
	req.True(topicNames["my-topic"]) // Longest prefix match, not "my"
}

func TestResolveTopics_TopicRecordNameNoMatch(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"inventory"}

	subjects := []string{"orders-com.example.Order"}
	result, err := ResolveTopics(subjects, "topic-record-name", nil, false, mock, []string{"cluster-1"})
	req.NoError(err)
	req.Len(result, 0) // No matching topic
}

func TestResolveTopics_MultipleClusters(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.ClusterTopics["cluster-1"] = []string{"orders"}
	mock.ClusterTopics["cluster-2"] = []string{"orders"}

	subjects := []string{"orders-value"}
	result, err := ResolveTopics(subjects, "topic-name", nil, false, mock, []string{"cluster-1", "cluster-2"})
	req.NoError(err)
	req.Len(result, 2) // Same topic on two clusters
}
