package pkg

import (
	"fmt"
	"strconv"
	"strings"
)

// AnalyzeCandidates runs all safety checks on deletion candidates and returns
// annotated DeletionCandidates with status and block reasons.
func AnalyzeCandidates(schemas []SchemaInfo, activeSchemas map[int32]int, platform Platform, strategy string) ([]DeletionCandidate, error) {
	// Step 1: Identify unused schemas
	var unused []SchemaInfo
	for _, schema := range schemas {
		schemaID, err := strconv.ParseInt(schema.SchemaID.String(), 10, 32)
		if err != nil {
			continue
		}
		if IsKeySchema(schema.Subject) {
			if activeSchemas[int32(schemaID)]&KEYONLY == 0 {
				unused = append(unused, schema)
			}
		} else if IsValueSchema(schema.Subject) {
			if activeSchemas[int32(schemaID)]&VALUEONLY == 0 {
				unused = append(unused, schema)
			}
		} else {
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
		id, err := strconv.Atoi(c.SchemaID)
		if err != nil {
			continue
		}
		candidateIDs[id] = true
	}

	// Step 2: Check references
	candidates = checkReferences(candidates, candidateIDs, platform)

	// Step 3: Check rules (migration chain, encryption, domain)
	candidates = checkRules(candidates, schemas, platform)

	// Step 4: Check global rule inheritance and global encryption
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
	subjectCandidates := make(map[string][]int)
	for i, c := range candidates {
		subjectCandidates[c.Subject] = append(subjectCandidates[c.Subject], i)
	}

	candidateVersions := make(map[string]map[string]bool)
	for _, c := range candidates {
		if candidateVersions[c.Subject] == nil {
			candidateVersions[c.Subject] = make(map[string]bool)
		}
		candidateVersions[c.Subject][c.Version] = true
	}

	allVersionsBySubject := make(map[string][]string)
	for _, s := range allSchemas {
		allVersionsBySubject[s.Subject] = append(allVersionsBySubject[s.Subject], s.Version.String())
	}

	for subject, indices := range subjectCandidates {
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

			// Check encryption rules in BOTH domain and migration rules (hard block)
			if hasEncryptionRules(detail.RuleSet) {
				candidates[idx].Status = StatusBlockedByEncryption
				for _, rule := range getAllEncryptionRules(detail.RuleSet) {
					candidates[idx].BlockReasons = append(candidates[idx].BlockReasons,
						fmt.Sprintf("Has %s rule '%s' - deleting will make encrypted messages unreadable", rule.Type, rule.Name))
				}
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

// hasEncryptionRules checks both domain and migration rules for ENCRYPT/DECRYPT.
func hasEncryptionRules(rs *RuleSet) bool {
	for _, r := range rs.DomainRules {
		if strings.EqualFold(r.Type, "ENCRYPT") || strings.EqualFold(r.Type, "DECRYPT") {
			return true
		}
	}
	for _, r := range rs.MigrationRules {
		if strings.EqualFold(r.Type, "ENCRYPT") || strings.EqualFold(r.Type, "DECRYPT") {
			return true
		}
	}
	return false
}

// getAllEncryptionRules returns all ENCRYPT/DECRYPT rules from both domain and migration.
func getAllEncryptionRules(rs *RuleSet) []Rule {
	var result []Rule
	for _, r := range rs.DomainRules {
		if strings.EqualFold(r.Type, "ENCRYPT") || strings.EqualFold(r.Type, "DECRYPT") {
			result = append(result, r)
		}
	}
	for _, r := range rs.MigrationRules {
		if strings.EqualFold(r.Type, "ENCRYPT") || strings.EqualFold(r.Type, "DECRYPT") {
			result = append(result, r)
		}
	}
	return result
}

// checkMigrationChain verifies that deleting candidates won't break migration paths
// between active versions. Blocks candidates that sit between active versions
// if ANY version in the subject has migration rules (the candidate may be a
// migration target even if it doesn't carry rules itself).
func checkMigrationChain(candidates []DeletionCandidate, indices []int, subject string, allVersions []string, candidateVersions map[string]bool, platform Platform) {
	// Check if any version in this subject has migration rules
	anyMigrationRules := false
	for _, v := range allVersions {
		detail, err := platform.GetSchemaDetail(subject, v)
		if err != nil || detail == nil || detail.RuleSet == nil {
			continue
		}
		if len(detail.RuleSet.MigrationRules) > 0 {
			anyMigrationRules = true
			break
		}
	}

	if !anyMigrationRules {
		return
	}

	// Find active versions (not candidates)
	var activeVersions []string
	for _, v := range allVersions {
		if !candidateVersions[v] {
			activeVersions = append(activeVersions, v)
		}
	}

	if len(activeVersions) == 0 {
		return
	}

	// Block any candidate that sits between active versions when migration rules exist.
	// Even versions without rules themselves can be migration targets.
	for _, idx := range indices {
		if candidates[idx].IsBlocked() {
			continue
		}
		v := candidates[idx].Version
		vNum, err := strconv.Atoi(v)
		if err != nil {
			continue
		}

		hasLower := false
		hasHigher := false
		for _, av := range activeVersions {
			avNum, err := strconv.Atoi(av)
			if err != nil {
				continue
			}
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

	if globalConfig.DefaultRuleSet == nil {
		return candidates
	}

	hasGlobalRules := len(globalConfig.DefaultRuleSet.DomainRules) > 0 || len(globalConfig.DefaultRuleSet.MigrationRules) > 0
	if !hasGlobalRules {
		return candidates
	}

	// Check if global rules include encryption
	globalHasEncryption := hasEncryptionRules(globalConfig.DefaultRuleSet)

	for i := range candidates {
		if candidates[i].IsBlocked() {
			continue
		}

		subjectConfig, err := platform.GetSubjectConfig(candidates[i].Subject)
		if err != nil {
			continue
		}

		// Subject inherits global rules if it has no override
		if subjectConfig.OverrideRuleSet == nil && subjectConfig.DefaultRuleSet == nil {
			candidates[i].InheritsGlobalRules = true

			// If global rules have encryption and subject inherits them, block
			if globalHasEncryption {
				candidates[i].Status = StatusBlockedByEncryption
				candidates[i].BlockReasons = append(candidates[i].BlockReasons,
					"Inherits global ENCRYPT/DECRYPT rules - deleting may make encrypted messages unreadable")
			}
		}
	}

	return candidates
}

func checkRuleReferences(candidates []DeletionCandidate, allSchemas []SchemaInfo, unused []SchemaInfo, platform Platform) []DeletionCandidate {
	unusedSet := make(map[string]bool)
	for _, u := range unused {
		unusedSet[u.Subject+":"+u.Version.String()] = true
	}

	// Build candidate key map for O(1) lookup instead of O(C) inner loop
	candidateByKey := make(map[string]int) // "subject:version" -> index
	for i, c := range candidates {
		candidateByKey[c.Subject+":"+c.Version] = i
	}

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
			if idx, ok := candidateByKey[ref]; ok {
				if !candidates[idx].IsBlocked() {
					candidates[idx].Status = StatusBlockedByRuleReference
					candidates[idx].BlockReasons = append(candidates[idx].BlockReasons,
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
	allRules := make([]Rule, 0, len(ruleSet.DomainRules)+len(ruleSet.MigrationRules))
	allRules = append(allRules, ruleSet.DomainRules...)
	allRules = append(allRules, ruleSet.MigrationRules...)
	for _, rule := range allRules {
		for _, v := range rule.Params {
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
		if candidates[i].IsBlocked() {
			continue
		}
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
				if candidates[j].Subject == subject && !candidates[j].IsBlocked() {
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
