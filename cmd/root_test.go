package cmd

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeActiveSchemas_AllSucceed(t *testing.T) {
	req := require.New(t)
	active, failed := mergeActiveSchemas([]scanResult{
		{topic: "a", schemas: map[int32]int{1: 2, 2: 1}},
		{topic: "b", schemas: map[int32]int{2: 2, 3: 2}},
	})
	req.Empty(failed)
	// Schema 2 is KEYONLY(1) in a and VALUEONLY(2) in b -> OR-merged to 3.
	req.Equal(map[int32]int{1: 2, 2: 3, 3: 2}, active)
}

func TestMergeActiveSchemas_CollectsAllFailures(t *testing.T) {
	req := require.New(t)
	active, failed := mergeActiveSchemas([]scanResult{
		{topic: "a", schemas: map[int32]int{1: 2}},
		{topic: "b", err: errors.New("auth error")},
		{topic: "c", err: errors.New("broker down")},
	})
	// All failures are reported, not just the first.
	req.ElementsMatch([]string{"b", "c"}, failed)
	// Successful topics still contribute their schemas.
	req.Equal(map[int32]int{1: 2}, active)
}

func TestMergeActiveSchemas_AllFail(t *testing.T) {
	req := require.New(t)
	active, failed := mergeActiveSchemas([]scanResult{
		{topic: "a", err: errors.New("boom")},
		{topic: "b", err: errors.New("boom")},
	})
	req.ElementsMatch([]string{"a", "b"}, failed)
	// No schema is active, so nothing is protected by activity — every subject
	// must instead be protected via the failedTopics path.
	req.Empty(active)
	req.NotNil(active)
}

func TestMergeActiveSchemas_DedupsFailuresAcrossClusters(t *testing.T) {
	req := require.New(t)
	// Same topic name fails in two clusters; it should appear once, first-seen order.
	_, failed := mergeActiveSchemas([]scanResult{
		{topic: "orders", cluster: "lkc-1", err: errors.New("boom")},
		{topic: "payments", cluster: "lkc-1", err: errors.New("boom")},
		{topic: "orders", cluster: "lkc-2", err: errors.New("boom")},
	})
	req.Equal([]string{"orders", "payments"}, failed)
}

func TestRunScan_RejectsNonPositiveTimeout(t *testing.T) {
	req := require.New(t)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"scan", "--all-subjects", "--scan-timeout=0"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	req.Error(err)
	req.Contains(err.Error(), "scan-timeout must be positive")
}
