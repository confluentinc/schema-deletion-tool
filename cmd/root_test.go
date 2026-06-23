package cmd

import (
	"errors"
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
