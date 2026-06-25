package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubjectsForFailedTopics_NoFailures(t *testing.T) {
	req := require.New(t)
	got := SubjectsForFailedTopics(nil, []string{"orders-value"}, []string{"orders"}, "topic-name")
	req.Empty(got)
}

func TestSubjectsForFailedTopics_TopicName(t *testing.T) {
	req := require.New(t)
	subjects := []string{"orders-value", "orders-key", "payments-value"}
	got := SubjectsForFailedTopics([]string{"orders"}, subjects, []string{"orders", "payments"}, "topic-name")
	req.Equal(map[string]bool{"orders-value": true, "orders-key": true}, got)
}

func TestSubjectsForFailedTopics_TopicNameWithContext(t *testing.T) {
	req := require.New(t)
	subjects := []string{":.ctx:orders-value", "payments-value"}
	got := SubjectsForFailedTopics([]string{"orders"}, subjects, nil, "topic-name")
	req.Equal(map[string]bool{":.ctx:orders-value": true}, got)
}

func TestSubjectsForFailedTopics_DefaultStrategyTreatedAsTopicName(t *testing.T) {
	req := require.New(t)
	subjects := []string{"orders-value", "orders-key", "payments-value"}
	// An empty/unknown strategy falls through to topic-name behavior, matching
	// ResolveTopics which defaults an unset strategy to topic-name.
	got := SubjectsForFailedTopics([]string{"orders"}, subjects, []string{"orders", "payments"}, "")
	req.Equal(map[string]bool{"orders-value": true, "orders-key": true}, got)
}

func TestSubjectsForFailedTopics_RecordNameBlocksAll(t *testing.T) {
	req := require.New(t)
	subjects := []string{"com.example.Order", "com.example.Payment"}
	// Subjects aren't tied to topics by name, so any failure taints every subject.
	got := SubjectsForFailedTopics([]string{"some-topic"}, subjects, []string{"some-topic"}, "record-name")
	req.Equal(map[string]bool{"com.example.Order": true, "com.example.Payment": true}, got)
}

func TestSubjectsForFailedTopics_TopicRecordNameLongestPrefix(t *testing.T) {
	req := require.New(t)
	topics := []string{"orders", "orders-eu"}
	subjects := []string{"orders-eu-com.example.Order", "orders-com.example.Payment"}
	// Only the subject whose longest-prefix topic ("orders-eu") failed is unverified.
	got := SubjectsForFailedTopics([]string{"orders-eu"}, subjects, topics, "topic-record-name")
	req.Equal(map[string]bool{"orders-eu-com.example.Order": true}, got)
}

func TestMarkUnverified(t *testing.T) {
	req := require.New(t)
	candidates := []DeletionCandidate{
		{Subject: "orders-value", Version: "1", SchemaID: "10", Status: StatusSafe},
		{Subject: "payments-value", Version: "1", SchemaID: "20", Status: StatusSafe},
	}
	got := MarkUnverified(candidates, map[string]bool{"orders-value": true})

	req.Equal(StatusBlockedUnverified, got[0].Status)
	req.True(got[0].IsBlocked())
	req.NotEmpty(got[0].BlockReasons)
	// Untouched subject stays safe.
	req.Equal(StatusSafe, got[1].Status)
	req.False(got[1].IsBlocked())
}

func TestMarkUnverified_PreservesSpecificBlockStatus(t *testing.T) {
	req := require.New(t)
	candidates := []DeletionCandidate{
		{Subject: "orders-value", Status: StatusBlockedByReferences,
			BlockReasons: []string{"Referenced by active schema IDs: [300]"}},
	}
	got := MarkUnverified(candidates, map[string]bool{"orders-value": true})

	req.Equal(StatusBlockedByReferences, got[0].Status)
	req.Len(got[0].BlockReasons, 2)
	req.Contains(got[0].BlockReasons[1], "could not be scanned")
}

func TestMarkUnverified_UpgradesWarnedToBlocked(t *testing.T) {
	req := require.New(t)
	candidates := []DeletionCandidate{
		{Subject: "orders-value", Status: StatusWarnHasDomainRules},
	}
	got := MarkUnverified(candidates, map[string]bool{"orders-value": true})

	// A warned candidate is deletable-with-confirmation, so it must be blocked.
	req.Equal(StatusBlockedUnverified, got[0].Status)
	req.True(got[0].IsBlocked())
}

func TestMarkUnverified_NoUnverified(t *testing.T) {
	req := require.New(t)
	candidates := []DeletionCandidate{{Subject: "orders-value", Status: StatusSafe}}
	got := MarkUnverified(candidates, nil)
	req.Equal(StatusSafe, got[0].Status)
}
