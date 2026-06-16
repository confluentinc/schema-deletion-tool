package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// WriteManifest writes the analysis results to a JSON manifest file.
func WriteManifest(path string, candidates []DeletionCandidate, opts ManifestOptions) error {
	activeIDs := make([]int32, 0, len(opts.ActiveSchemaIDs))
	for id := range opts.ActiveSchemaIDs {
		activeIDs = append(activeIDs, id)
	}
	sort.Slice(activeIDs, func(i, j int) bool { return activeIDs[i] < activeIDs[j] })

	manifest := Manifest{
		ManifestVersion: "1",
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Platform:        opts.Platform,
		Strategy:        opts.Strategy,
		SRUrl:           opts.SRUrl,
		Environment:     opts.Environment,
		Candidates:      candidates,
		ScannedTopics:   opts.ScannedTopics,
		ScannedClusters: opts.ScannedClusters,
		ActiveSchemaIDs: activeIDs,
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err = os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write manifest to %s: %w", path, err)
	}

	fmt.Printf("Manifest written to %s (%d candidates)\n", path, len(candidates))
	return nil
}

// ReadManifest reads and validates a manifest file.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest file: %w", err)
	}

	// Validate entries
	for i, c := range manifest.Candidates {
		if c.Subject == "" {
			return nil, fmt.Errorf("candidate %d: subject is required", i)
		}
		if c.Version == "" {
			return nil, fmt.Errorf("candidate %d: version is required", i)
		}
		if c.SchemaID == "" {
			return nil, fmt.Errorf("candidate %d: schema_id is required", i)
		}
	}

	// Warn about stale manifests
	generatedAt, err := time.Parse(time.RFC3339, manifest.GeneratedAt)
	if err == nil {
		age := time.Since(generatedAt)
		if age > 7*24*time.Hour {
			fmt.Printf("%sWarning: manifest is %.0f days old. Consider re-running with --dry-run.%s\n",
				RED, age.Hours()/24, RESET)
		}
	}

	return &manifest, nil
}

// GetSafeCandidates returns only candidates with status "safe".
func GetSafeCandidates(candidates []DeletionCandidate) []DeletionCandidate {
	var safe []DeletionCandidate
	for _, c := range candidates {
		if c.Status == StatusSafe {
			safe = append(safe, c)
		}
	}
	return safe
}

// GetBlockedCandidates returns candidates that are blocked from deletion.
func GetBlockedCandidates(candidates []DeletionCandidate) []DeletionCandidate {
	var blocked []DeletionCandidate
	for _, c := range candidates {
		if c.IsBlocked() {
			blocked = append(blocked, c)
		}
	}
	return blocked
}

// GetWarnedCandidates returns candidates that have warnings but can be deleted.
func GetWarnedCandidates(candidates []DeletionCandidate) []DeletionCandidate {
	var warned []DeletionCandidate
	for _, c := range candidates {
		if c.IsWarning() {
			warned = append(warned, c)
		}
	}
	return warned
}

// ManifestOptions holds metadata for manifest generation.
type ManifestOptions struct {
	Platform        string
	Strategy        string
	SRUrl           string
	Environment     string
	ScannedTopics   []string
	ScannedClusters []string
	ActiveSchemaIDs map[int32]int
}

// ExecuteDeletion performs soft-delete and/or hard-delete on candidates.
func ExecuteDeletion(candidates []DeletionCandidate, platform Platform, softDelete, hardDelete, force bool) error {
	safe := GetSafeCandidates(candidates)
	warned := GetWarnedCandidates(candidates)
	blocked := GetBlockedCandidates(candidates)

	if len(blocked) > 0 {
		fmt.Printf("\n%s%d schema(s) BLOCKED from deletion:%s\n", RED, len(blocked), RESET)
		for _, c := range blocked {
			fmt.Printf("  %s:%s (ID %s) - %s: %s\n", c.Subject, c.Version, c.SchemaID, c.Status, joinReasons(c.BlockReasons))
		}
	}

	deletable := safe
	if len(warned) > 0 {
		fmt.Printf("\n%d schema(s) have warnings:\n", len(warned))
		for _, c := range warned {
			fmt.Printf("  %s:%s (ID %s) - %s: %s\n", c.Subject, c.Version, c.SchemaID, c.Status, joinReasons(c.BlockReasons))
		}
		if force {
			deletable = append(deletable, warned...)
		} else {
			fmt.Print("Include warned schemas in deletion? [y/N]: ")
			resp, err := ReadLine()
			if err != nil {
				return err
			}
			if IsYes(resp) {
				deletable = append(deletable, warned...)
			}
		}
	}

	if len(deletable) == 0 {
		fmt.Println("No schemas eligible for deletion.")
		return nil
	}

	fmt.Printf("\n%s%d schema(s) eligible for deletion:%s\n", GREEN, len(deletable), RESET)
	for _, c := range deletable {
		fmt.Printf("  %s:%s (ID %s)\n", c.Subject, c.Version, c.SchemaID)
	}

	if softDelete {
		if !force {
			fmt.Printf("\nConfirm %ssoft deletion%s of %d schemas [y/N]: ", RED, RESET, len(deletable))
			resp, err := ReadLine()
			if err != nil {
				return err
			}
			if !IsYes(resp) {
				fmt.Println("Soft deletion cancelled.")
				return nil
			}
		}

		fmt.Println("Executing soft deletion...")
		for _, c := range deletable {
			if err := platform.DeleteSchema(c.Subject, c.Version, false); err != nil {
				return fmt.Errorf("failed to soft-delete %s:%s: %w", c.Subject, c.Version, err)
			}
		}
		fmt.Printf("Soft-deleted %d schema(s).\n", len(deletable))
	}

	if hardDelete {
		if !force {
			fmt.Printf("\nConfirm %shard deletion%s of %d schemas (PERMANENT, cannot be recovered) [y/N]: %s", RED, RESET, len(deletable), RED)
			resp, err := ReadLine()
			ResetColor()
			if err != nil {
				return err
			}
			if !IsYes(resp) {
				fmt.Println("Hard deletion cancelled.")
				return nil
			}
		}

		fmt.Println("Executing hard deletion...")
		hardDeleted := 0
		for _, c := range deletable {
			if err := platform.DeleteSchema(c.Subject, c.Version, true); err != nil {
				fmt.Printf("%sWarning: failed to hard-delete %s:%s: %v%s\n", RED, c.Subject, c.Version, err, RESET)
				continue
			}
			hardDeleted++
		}
		fmt.Printf("Hard-deleted %d of %d schema(s).\n", hardDeleted, len(deletable))
	}

	return nil
}

func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	result := reasons[0]
	for i := 1; i < len(reasons); i++ {
		result += "; " + reasons[i]
	}
	return result
}
