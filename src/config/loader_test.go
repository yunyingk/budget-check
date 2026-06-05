package config

import (
	"budget/src/types"
	"testing"
)

func TestValidateRules_AllowsCommit(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{{
			ID:   "T1",
			Name: "测试包",
			Steps: []types.Step{{
				When: `项目 == "P1"`,
				Then: "commit",
			}},
		}},
	}
	if err := ValidateRules(cfg); err != nil {
		t.Fatalf("expected commit to validate: %v", err)
	}
}

func TestValidateRules_RejectsInvalidThen(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{{
			ID:   "T1",
			Name: "测试包",
			Steps: []types.Step{{
				Then: "invalid",
			}},
		}},
	}
	if err := ValidateRules(cfg); err == nil {
		t.Fatal("expected invalid then to fail validation")
	}
}
