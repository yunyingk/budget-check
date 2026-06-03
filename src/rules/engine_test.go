package rules

import (
	"budget/src/types"
	"testing"
)

func TestNewEngine_Compiles(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{
			{ID: "T1", Name: "成本中心", Steps: []types.Step{
				{When: `u_费用性质 not in ['X','Y']`, Then: "pass"},
				{Action: "match_info_to_budget"},
			}},
			{ID: "T2", Name: "项目", Steps: []types.Step{
				{When: `u_费用性质 == 'ID01LPDfjPcnyn'`},
			}},
		},
	}
	if _, err := NewEngine(nil, nil, cfg); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

func TestNewEngine_BadExpr(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "坏规则", Steps: []types.Step{{When: `this is not valid`}},
		}},
	}
	if _, err := NewEngine(nil, nil, cfg); err == nil {
		t.Fatal("expected compile error")
	}
}

func TestNewEngine_NilConfig(t *testing.T) {
	e, err := NewEngine(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.targets) != 0 {
		t.Fatal("expected empty targets")
	}
}

func TestRunTarget_WhenFalsePass(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包", Steps: []types.Step{
				{When: `u_费用性质 not in ['X','Y']`, Then: "pass"},
			},
		}},
	}
	e, err := NewEngine(nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	form := map[string]interface{}{"u_费用性质": "Z"}
	unit := CheckUnit{Label: "明细1"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("expected pass (empty msg), got %q", msg)
	}
}

func TestRunTarget_WhenTrueContinues(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包", Steps: []types.Step{
				{When: `u_费用性质 == 'X'`},
			},
		}},
	}
	e, err := NewEngine(nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	form := map[string]interface{}{"u_费用性质": "X"}
	unit := CheckUnit{Label: "明细1"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("expected no refusal, got %q", msg)
	}
}

func TestRunTarget_CompoundExpr(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试", Steps: []types.Step{
				{When: `u_费用性质 in ['A','B'] && 项目 == 'P1'`, Then: "pass"},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg)
	form := map[string]interface{}{"u_费用性质": "A", "项目": "P1"}
	unit := CheckUnit{Label: "X"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("expected pass, got %q", msg)
	}
	form["项目"] = "P2"
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("when=false && then=pass should pass, got %q", msg)
	}
}
