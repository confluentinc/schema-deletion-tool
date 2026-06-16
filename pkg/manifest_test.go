package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteAndReadManifest(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	candidates := []DeletionCandidate{
		{Subject: "orders-value", Version: "1", SchemaID: "100", Status: StatusSafe},
		{Subject: "users-value", Version: "2", SchemaID: "200", Status: StatusBlockedByReferences,
			BlockReasons: []string{"Referenced by active schema IDs: [300]"}, ReferencedBy: []int{300}},
		{Subject: "payments-value", Version: "3", SchemaID: "300", Status: StatusWarnHasDomainRules,
			BlockReasons: []string{"Has domain rules: validateAge"},
			Rules: &RuleSet{DomainRules: []Rule{{Name: "validateAge", Type: "CEL"}}}},
	}

	opts := ManifestOptions{
		Platform:        "cloud",
		Strategy:        "topic-name",
		ScannedTopics:   []string{"orders", "users", "payments"},
		ScannedClusters: []string{"lkc-123"},
		ActiveSchemaIDs: map[int32]int{100: VALUEONLY, 300: VALUEONLY},
	}

	err := WriteManifest(path, candidates, opts)
	req.NoError(err)

	manifest, err := ReadManifest(path)
	req.NoError(err)
	req.Equal("cloud", manifest.Platform)
	req.Equal("topic-name", manifest.Strategy)
	req.Len(manifest.Candidates, 3)
	req.Equal(StatusSafe, manifest.Candidates[0].Status)
	req.Equal(StatusBlockedByReferences, manifest.Candidates[1].Status)
	req.Equal(StatusWarnHasDomainRules, manifest.Candidates[2].Status)
	req.Equal([]int{300}, manifest.Candidates[1].ReferencedBy)
	req.Equal("validateAge", manifest.Candidates[2].Rules.DomainRules[0].Name)
	req.Len(manifest.ScannedTopics, 3)
	req.Len(manifest.ScannedClusters, 1)
}

func TestReadManifest_MissingSubject(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	manifest := Manifest{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Candidates: []DeletionCandidate{
			{Version: "1", SchemaID: "100", Status: StatusSafe},
		},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(path, data, 0644)

	_, err := ReadManifest(path)
	req.Error(err)
	req.Contains(err.Error(), "subject is required")
}

func TestReadManifest_MissingVersion(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	manifest := Manifest{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Candidates: []DeletionCandidate{
			{Subject: "orders-value", SchemaID: "100", Status: StatusSafe},
		},
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(path, data, 0644)

	_, err := ReadManifest(path)
	req.Error(err)
	req.Contains(err.Error(), "version is required")
}

func TestReadManifest_InvalidJSON(t *testing.T) {
	req := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	os.WriteFile(path, []byte("{bad json"), 0644)

	_, err := ReadManifest(path)
	req.Error(err)
	req.Contains(err.Error(), "failed to parse")
}

func TestReadManifest_FileNotFound(t *testing.T) {
	req := require.New(t)
	_, err := ReadManifest("/nonexistent/manifest.json")
	req.Error(err)
	req.Contains(err.Error(), "failed to read")
}

func TestGetSafeCandidates(t *testing.T) {
	req := require.New(t)
	candidates := []DeletionCandidate{
		{Status: StatusSafe},
		{Status: StatusBlockedByReferences},
		{Status: StatusSafe},
		{Status: StatusWarnHasDomainRules},
		{Status: StatusBlockedByEncryption},
	}

	safe := GetSafeCandidates(candidates)
	req.Len(safe, 2)

	blocked := GetBlockedCandidates(candidates)
	req.Len(blocked, 2)

	warned := GetWarnedCandidates(candidates)
	req.Len(warned, 1)
}

func TestDeletionCandidate_IsBlocked(t *testing.T) {
	req := require.New(t)
	req.True((&DeletionCandidate{Status: StatusBlockedByReferences}).IsBlocked())
	req.True((&DeletionCandidate{Status: StatusBlockedByMigrationChain}).IsBlocked())
	req.True((&DeletionCandidate{Status: StatusBlockedByEncryption}).IsBlocked())
	req.True((&DeletionCandidate{Status: StatusBlockedByRuleReference}).IsBlocked())
	req.False((&DeletionCandidate{Status: StatusSafe}).IsBlocked())
	req.False((&DeletionCandidate{Status: StatusWarnHasDomainRules}).IsBlocked())
	req.False((&DeletionCandidate{Status: StatusWarnHasMigrationRules}).IsBlocked())
}

func TestDeletionCandidate_IsWarning(t *testing.T) {
	req := require.New(t)
	req.True((&DeletionCandidate{Status: StatusWarnHasDomainRules}).IsWarning())
	req.True((&DeletionCandidate{Status: StatusWarnHasMigrationRules}).IsWarning())
	req.False((&DeletionCandidate{Status: StatusSafe}).IsWarning())
	req.False((&DeletionCandidate{Status: StatusBlockedByReferences}).IsWarning())
}
