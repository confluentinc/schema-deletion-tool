package pkg

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckReferences_NoRefs(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{} // none active

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusSafe, candidates[0].Status)
}

func TestCheckReferences_ActiveRef(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	// Schema 100 is referenced by schema 200 (which is active, not a candidate)
	mock.References["orders-value:1"] = []int{200}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusBlockedByReferences, candidates[0].Status)
	req.Contains(candidates[0].BlockReasons[0], "200")
}

func TestCheckReferences_AllRefsAlsoCandidates(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	// Schema 100 is referenced by schema 101, both are unused candidates
	mock.References["orders-value:1"] = []int{101}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 2)
	// Both should be safe since all referencing schemas are also candidates
	req.Equal(StatusSafe, candidates[0].Status)
	req.Equal(StatusSafe, candidates[1].Status)
}

func TestCheckReferences_MixedRefs(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	// Schema 100 is referenced by 101 (candidate) and 200 (active)
	mock.References["orders-value:1"] = []int{101, 200}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 2)
	req.Equal(StatusBlockedByReferences, candidates[0].Status)
	req.Equal(StatusSafe, candidates[1].Status)
}

func TestCheckRules_EncryptionBlock(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SchemaDetails["users-value:1"] = &SchemaDetail{
		RuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "encryptSSN", Type: "ENCRYPT", Kind: "TRANSFORM"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "users-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusBlockedByEncryption, candidates[0].Status)
	req.Contains(candidates[0].BlockReasons[0], "ENCRYPT")
}

func TestCheckRules_DecryptBlock(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SchemaDetails["users-value:1"] = &SchemaDetail{
		RuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "decryptSSN", Type: "DECRYPT", Kind: "TRANSFORM"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "users-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusBlockedByEncryption, candidates[0].Status)
}

func TestCheckRules_DomainRulesWarning(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SchemaDetails["orders-value:1"] = &SchemaDetail{
		RuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "validateAge", Type: "CEL", Kind: "CONDITION"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusWarnHasDomainRules, candidates[0].Status)
}

func TestCheckRules_MigrationRulesWarning(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SchemaDetails["orders-value:3"] = &SchemaDetail{
		RuleSet: &RuleSet{
			MigrationRules: []Rule{
				{Name: "upgradeFromV2", Type: "JSONATA", Kind: "TRANSFORM", Mode: "UPGRADE"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("3"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusWarnHasMigrationRules, candidates[0].Status)
}

func TestCheckRules_NoRules(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SchemaDetails["orders-value:1"] = &SchemaDetail{
		RuleSet: &RuleSet{
			DomainRules:    []Rule{},
			MigrationRules: []Rule{},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal(StatusSafe, candidates[0].Status)
}

func TestMigrationChain_MiddleUnused(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	// v1 active, v2 candidate with migration rules, v3 active
	mock.SchemaDetails["orders-value:1"] = &SchemaDetail{}
	mock.SchemaDetails["orders-value:2"] = &SchemaDetail{
		RuleSet: &RuleSet{
			MigrationRules: []Rule{
				{Name: "upgrade", Type: "JSONATA", Kind: "TRANSFORM", Mode: "UPGRADE"},
			},
		},
	}
	mock.SchemaDetails["orders-value:3"] = &SchemaDetail{}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
		{Subject: "orders-value", Version: json.Number("3"), SchemaID: json.Number("102")},
	}
	// v1 and v3 are active (found in messages)
	activeSchemas := map[int32]int{100: VALUEONLY, 102: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1) // Only v2 is a candidate
	req.Equal("2", candidates[0].Version)
	req.Equal(StatusBlockedByMigrationChain, candidates[0].Status)
}

func TestMigrationChain_EndUnused(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	// v1 active, v2 active, v3 candidate with migration rules
	mock.SchemaDetails["orders-value:1"] = &SchemaDetail{}
	mock.SchemaDetails["orders-value:2"] = &SchemaDetail{}
	mock.SchemaDetails["orders-value:3"] = &SchemaDetail{
		RuleSet: &RuleSet{
			MigrationRules: []Rule{
				{Name: "upgrade", Type: "JSONATA", Kind: "TRANSFORM", Mode: "UPGRADE"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
		{Subject: "orders-value", Version: json.Number("3"), SchemaID: json.Number("102")},
	}
	activeSchemas := map[int32]int{100: VALUEONLY, 101: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal("3", candidates[0].Version)
	// v3 is at the end, no active version after it. Should be warned, not blocked.
	req.NotEqual(StatusBlockedByMigrationChain, candidates[0].Status)
}

func TestMigrationChain_NoMigrationRules(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
		{Subject: "orders-value", Version: json.Number("3"), SchemaID: json.Number("102")},
	}
	activeSchemas := map[int32]int{100: VALUEONLY, 102: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal("2", candidates[0].Version)
	req.Equal(StatusSafe, candidates[0].Status) // No migration rules, safe to delete
}

func TestGlobalRules_Inherited(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.GlobalCfg = &GlobalConfig{
		DefaultRuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "globalValidation", Type: "CEL", Kind: "CONDITION"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.True(candidates[0].InheritsGlobalRules)
}

func TestGlobalRules_OverriddenBySubject(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.GlobalCfg = &GlobalConfig{
		DefaultRuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "globalValidation", Type: "CEL", Kind: "CONDITION"},
			},
		},
	}
	mock.SubjectConfigs["orders-value"] = &SubjectConfig{
		OverrideRuleSet: &RuleSet{
			DomainRules: []Rule{
				{Name: "subjectValidation", Type: "CEL", Kind: "CONDITION"},
			},
		},
	}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
	}
	activeSchemas := map[int32]int{}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.False(candidates[0].InheritsGlobalRules)
}

func TestRuleReferences_ActiveRuleRefersToCandidate(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	// Active schema v1 has a migration rule that references candidate v2
	mock.SchemaDetails["orders-value:1"] = &SchemaDetail{
		RuleSet: &RuleSet{
			MigrationRules: []Rule{
				{
					Name: "upgradeToV2",
					Type: "JSONATA",
					Kind: "TRANSFORM",
					Params: map[string]string{
						"targetSchema": "orders-value:2",
					},
				},
			},
		},
	}
	mock.SchemaDetails["orders-value:2"] = &SchemaDetail{}

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
	}
	// v1 is active
	activeSchemas := map[int32]int{100: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1) // Only v2 is unused
	req.Equal("2", candidates[0].Version)
	req.Equal(StatusBlockedByRuleReference, candidates[0].Status)
}

func TestCheckCompatibility_Transitive(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SubjectConfigs["orders-value"] = &SubjectConfig{
		CompatibilityLevel: "FULL_TRANSITIVE",
	}

	candidates := []DeletionCandidate{
		{Subject: "orders-value", Version: "2", SchemaID: "101", Status: StatusSafe},
	}

	result := CheckCompatibility(candidates, mock)
	req.Len(result, 1)
	req.Contains(result[0].BlockReasons[0], "FULL_TRANSITIVE")
}

func TestCheckCompatibility_NonTransitive(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.SubjectConfigs["orders-value"] = &SubjectConfig{
		CompatibilityLevel: "BACKWARD",
	}

	candidates := []DeletionCandidate{
		{Subject: "orders-value", Version: "2", SchemaID: "101", Status: StatusSafe},
	}

	result := CheckCompatibility(candidates, mock)
	req.Len(result, 1)
	req.Empty(result[0].BlockReasons)
}

func TestCheckCompatibility_GlobalDefault(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()
	mock.GlobalCfg = &GlobalConfig{
		Compatibility: "FORWARD_TRANSITIVE",
	}

	candidates := []DeletionCandidate{
		{Subject: "orders-value", Version: "2", SchemaID: "101", Status: StatusSafe},
	}

	result := CheckCompatibility(candidates, mock)
	req.Len(result, 1)
	req.Contains(result[0].BlockReasons[0], "FORWARD_TRANSITIVE")
}

func TestAnalyze_AllActive(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	schemas := []SchemaInfo{
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("2"), SchemaID: json.Number("101")},
	}
	activeSchemas := map[int32]int{100: VALUEONLY, 101: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 0)
}

func TestAnalyze_KeySchemaUsage(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	schemas := []SchemaInfo{
		{Subject: "orders-key", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "orders-value", Version: json.Number("1"), SchemaID: json.Number("101")},
	}
	// Key schema 100 is used as key, value schema 101 is not used as value
	activeSchemas := map[int32]int{100: KEYONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "topic-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal("orders-value", candidates[0].Subject)
}

func TestAnalyze_RecordNameStrategy(t *testing.T) {
	req := require.New(t)
	mock := NewMockPlatform()

	schemas := []SchemaInfo{
		{Subject: "com.example.Order", Version: json.Number("1"), SchemaID: json.Number("100")},
		{Subject: "com.example.Payment", Version: json.Number("1"), SchemaID: json.Number("101")},
	}
	// Only Order is active
	activeSchemas := map[int32]int{100: VALUEONLY}

	candidates, err := AnalyzeCandidates(schemas, activeSchemas, mock, "record-name")
	req.NoError(err)
	req.Len(candidates, 1)
	req.Equal("com.example.Payment", candidates[0].Subject)
}
