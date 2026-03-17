package pkg

import (
	"fmt"
	"strconv"
	"strings"
)

// AnalyzeCandidates runs all safety checks on deletion candidates and returns
// annotated DeletionCandidates with status and block reasons.
func AnalyzeCandidates(schemas []SchemaInfo, activeSchemas map[int32]int, platform Platform, strategy string) ([]DeletionCandidate, error) {
	// Step 1: Identify unused schemas (same logic as before)
	var unused []SchemaInfo
	for _, schema := range schemas {
		schemaID, _ := strconv.ParseInt(schema.SchemaID.String(), 10, 32)
		if IsKeySchema(schema.Subject) {
			if activeSchemas[int32(schemaID)]&KEYONLY == 0 {
				unused = append(unused, schema)
			}
		} else if IsValueSchema(schema.Subject) {
			if activeSchemas[int32(schemaID)]&VALUEONLY == 0 {
				unused = append(unused, schema)
			}
		} else {
			// RecordNameStrategy or TopicRecordNameStrategy: check both key and value
			if activeSchemas[int32(schemaID)] == 0 {
				unused = append(unused, schema)
			}
		}
	}

	// Build initial candidates
	candidates := make([]DeletionCandidate, len(unused))
	for i, s := range unused {
		candidates[i] = DeletionCandidate{
			Subject:  s.Subject,
			Version:  s.Version.String(),
			SchemaID: s.SchemaID.String(),
			Status:   StatusSafe,
		}
	}

	// Build a set of candidate schema IDs for cross-referencing
	candidateIDs := make(map[int]bool)
	for _, c := range candidates {
		id, _ := strconv.Atoi(c.SchemaID)
		candidateIDs[id] = true
	}

	// Step 2: Check references
	candidates = checkReferences(candidates, candidateIDs, platform)

	// Step 3: Check rules (migration chain, encryption, domain)
	candidates = checkRules(candidates, schemas, platform)

	// Step 4: Check global rule inheritance
	candidates = checkGlobalRules(candidates, platform)

	// Step 5: Check rule references from active schemas
	candidates = checkRuleReferences(candidates, schemas, unused, platform)

	return candidates, nil
}

func checkReferences(candidates []DeletionCandidate, candidateIDs map[int]bool, platform Platform) []DeletionCandidate {
	for i := range candidates {
		refs, err := platform.GetReferencedBy(candidates[i].Subject, candidates[i].Version)
		if err != nil || len(refs) == 0 {
			continue
		}

		// Check if ALL referencing schemas are also candidates
		allRefsCandidates := true
		var activeRefs []int
		for _, refID := range refs {
			if !candidateIDs[refID] {
				allRefsCandidates = false
				activeRefs = append(activeRefs, refID)
			}
		}

		if !allRefsCandidates {
			candidates[i].Status = StatusBlockedByReferences
			candidates[i].ReferencedBy = activeRefs
			candidates[i].BlockReasons = append(candidates[i].BlockReasons,
				fmt.Sprintf("Referenced by active schema IDs: %v", activeRefs))
		}
	}
	return candidates
}

func checkRules(candidates []DeletionCandidate, allSchemas []SchemaInfo, platform Platform) []DeletionCandidate {
	// Group candidates by subject for migration chain analysis
	subjectCandidates := make(map[string][]int) // subject -> indices in candidates
	for i, c := range candidates {
		subjectCandidates[c.Subject] = append(subjectCandidates[c.Subject], i)
	}

	// Build set of candidate versions per subject
	candidateVersions := make(map[string]map[string]bool)
	for _, c := range candidates {
		if candidateVersions[c.Subject] == nil {
			candidateVersions[c.Subject] = make(map[string]bool)
		}
		candidateVersions[c.Subject][c.Version] = true
	}

	// Get all versions per subject (including active ones) for migration chain analysis
	allVersionsBySubject := make(map[string][]string)
	for _, s := range allSchemas {
		allVersionsBySubject[s.Subject] = append(allVersionsBySubject[s.Subject], s.Version.String())
	}

	for subject, indices := range subjectCandidates {
		// Get schema details for each candidate in this subject
		for _, idx := range indices {
			if candidates[idx].IsBlocked() {
				continue
			}

			detail, err := platform.GetSchemaDetail(candidates[idx].Subject, candidates[idx].Version)
			if err != nil || detail == nil {
				continue
			}

			candidates[idx].Rules = detail.RuleSet

			if detail.RuleSet == nil {
				continue
			}

			// Check encryption rules (hard block)
			for _, rule := range detail.RuleSet.DomainRules {
				if strings.EqualFold(rule.Type, "ENCRYPT") || strings.EqualFold(rule.Type, "DECRYPT") {
					candidates[idx].Status = StatusBlockedByEncryption
					candidates[idx].BlockReasons = append(candidates[idx].BlockReasons,
						fmt.Sprintf("Has %s rule '%s' - deleting will make encrypted messages unreadable", rule.Type, rule.Name))
				}
			}
			if candidates[idx].Status == StatusBlockedByEncryption {
				continue
			}

			// Check domain rules (warning)
			if len(detail.RuleSet.DomainRules) > 0 {
				ruleNames := make([]string, len(detail.RuleSet.DomainRules))
				for j, r := range detail.RuleSet.DomainRules {
					ruleNames[j] = r.Name
				}
				if candidates[idx].Status == StatusSafe {
					candidates[idx].Status = StatusWarnHasDomainRules
				}
				candidates[idx].BlockReasons = append(candidates[idx].BlockReasons,
					fmt.Sprintf("Has domain rules: %s", strings.Join(ruleNames, ", ")))
			}

			// Check migration rules (warning, unless chain would break)
			if len(detail.RuleSet.MigrationRules) > 0 {
				if candidates[idx].Status == StatusSafe {
					candidates[idx].Status = StatusWarnHasMigrationRules
				}
			}
		}

		// Migration chain integrity check
		checkMigrationChain(candidates, indices, subject, allVersionsBySubject[subject], candidateVersions[subject], platform)
	}

	return candidates
}

// checkMigrationChain verifies that deleting candidates won't break migration paths
// between active versions.
func checkMigrationChain(candidates []DeletionCandidate, indices []int, subject string, allVersions []string, candidateVersions map[string]bool, platform Platform) {
	// Find which versions have migration rules
	versionHasMigration := make(map[string]bool)
	for _, v := range allVersions {
		detail, err := platform.GetSchemaDetail(subject, v)
		if err != nil || detail == nil || detail.RuleSet == nil {
			continue
		}
		if len(detail.RuleSet.MigrationRules) > 0 {
			versionHasMigration[v] = true
		}
	}

	if len(versionHasMigration) == 0 {
		return
	}

	// Find active versions (not candidates)
	var activeVersions []string
	for _, v := range allVersions {
		if !candidateVersions[v] {
			activeVersions = append(activeVersions, v)
		}
	}

	if len(activeVersions) < 2 {
		return
	}

	// For each candidate version that has migration rules and sits between two active versions,
	// it's on a migration path and must be blocked.
	for _, idx := range indices {
		if candidates[idx].IsBlocked() {
			continue
		}
		v := candidates[idx].Version
		if !versionHasMigration[v] {
			continue
		}

		vNum, _ := strconv.Atoi(v)
		hasLower := false
		hasHigher := false
		for _, av := range activeVersions {
			avNum, _ := strconv.Atoi(av)
			if avNum < vNum {
				hasLower = true
			}
			if avNum > vNum {
				hasHigher = true
			}
		}

		if hasLower && hasHigher {
			candidates[idx].Status = StatusBlockedByMigrationChain
			candidates[idx].BlockReasons = append(candidates[idx].BlockReasons,
				"Required for migration path between active schema versions")
		}
	}
}

func checkGlobalRules(candidates []DeletionCandidate, platform Platform) []DeletionCandidate {
	globalConfig, err := platform.GetGlobalConfig()
	if err != nil || globalConfig == nil {
		return candidates
	}

	hasGlobalRules := globalConfig.DefaultRuleSet != nil &&
		(len(globalConfig.DefaultRuleSet.DomainRules) > 0 || len(globalConfig.DefaultRuleSet.MigrationRules) > 0)

	if !hasGlobalRules {
		return candidates
	}

	for i := range candidates {
		subjectConfig, err := platform.GetSubjectConfig(candidates[i].Subject)
		if err != nil {
			continue
		}

		// Subject inherits global rules if it has no override
		if subjectConfig.OverrideRuleSet == nil && subjectConfig.DefaultRuleSet == nil {
			candidates[i].InheritsGlobalRules = true
		}
	}

	return candidates
}

func checkRuleReferences(candidates []DeletionCandidate, allSchemas []SchemaInfo, unused []SchemaInfo, platform Platform) []DeletionCandidate {
	// Build set of unused schema keys
	unusedSet := make(map[string]bool)
	for _, u := range unused {
		unusedSet[u.Subject+":"+u.Version.String()] = true
	}

	// Check if any active schema's rules reference a candidate
	for _, schema := range allSchemas {
		if unusedSet[schema.Subject+":"+schema.Version.String()] {
			continue
		}
		detail, err := platform.GetSchemaDetail(schema.Subject, schema.Version.String())
		if err != nil || detail == nil || detail.RuleSet == nil {
			continue
		}

		refsFromRules := extractSchemaRefsFromRules(detail.RuleSet)
		for _, ref := range refsFromRules {
			for i := range candidates {
				candidateKey := candidates[i].Subject + ":" + candidates[i].Version
				if ref == candidateKey && !candidates[i].IsBlocked() {
					candidates[i].Status = StatusBlockedByRuleReference
					candidates[i].BlockReasons = append(candidates[i].BlockReasons,
						fmt.Sprintf("Referenced by rule on active schema %s:%s", schema.Subject, schema.Version.String()))
				}
			}
		}
	}

	return candidates
}

// extractSchemaRefsFromRules finds subject:version references in rule params.
func extractSchemaRefsFromRules(ruleSet *RuleSet) []string {
	var refs []string
	allRules := append(ruleSet.DomainRules, ruleSet.MigrationRules...)
	for _, rule := range allRules {
		for _, v := range rule.Params {
			// Look for subject:version patterns
			if strings.Contains(v, ":") {
				parts := strings.SplitN(v, ":", 2)
				if len(parts) == 2 {
					if _, err := strconv.Atoi(parts[1]); err == nil {
						refs = append(refs, v)
					}
				}
			}
		}
	}
	return refs
}

// CheckCompatibility warns about transitive compatibility levels.
func CheckCompatibility(candidates []DeletionCandidate, platform Platform) []DeletionCandidate {
	checked := make(map[string]bool)
	globalConfig, _ := platform.GetGlobalConfig()

	for i := range candidates {
		subject := candidates[i].Subject
		if checked[subject] {
			continue
		}
		checked[subject] = true

		config, err := platform.GetSubjectConfig(subject)
		if err != nil {
			continue
		}

		level := config.CompatibilityLevel
		if level == "" && globalConfig != nil {
			level = globalConfig.Compatibility
		}

		if isTransitive(level) {
			for j := range candidates {
				if candidates[j].Subject == subject {
					candidates[j].BlockReasons = append(candidates[j].BlockReasons,
						fmt.Sprintf("Subject has %s compatibility - deleting intermediate versions may break future registrations", level))
				}
			}
		}
	}
	return candidates
}

func isTransitive(level string) bool {
	upper := strings.ToUpper(level)
	return upper == "FORWARD_TRANSITIVE" || upper == "BACKWARD_TRANSITIVE" || upper == "FULL_TRANSITIVE"
}
