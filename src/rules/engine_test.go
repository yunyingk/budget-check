package rules

import (
	"budget/src/budget"
	"budget/src/types"
	"testing"
)

func TestNewEngine_Compiles(t *testing.T) {
	cfg := &types.RulesConfig{
		Version: 1,
		Targets: []types.RuleTarget{
			{ID: "T1", Name: "成本中心", Steps: []types.Step{
				{When: `u_费用性质 in ['X','Y']`, Then: "pass"},
				{Action: "match_info_to_budget"},
			}},
			{ID: "T2", Name: "项目", Steps: []types.Step{
				{When: `u_费用性质 == 'ID01LPDfjPcnyn'`, Then: "pass"},
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

func TestNewEngine_GlobalSteps(t *testing.T) {
	cfg := &types.RulesConfig{
		GlobalSteps: []types.Step{
			{When: `u_费用性质 == 'A'`, Then: "pass", Action: "全局通过"},
		},
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试", Steps: []types.Step{{Action: "match_info_to_budget"}},
		}},
	}
	e, err := NewEngine(nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.globalSteps) != 1 {
		t.Fatalf("expected 1 global step, got %d", len(e.globalSteps))
	}
}

func TestRunTarget_WhenTruePass(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包", Steps: []types.Step{
				{When: `u_费用性质 in ['X','Y']`, Then: "pass"},
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
		t.Errorf("expected pass (empty msg), got %q", msg)
	}
}

func TestRunTarget_WhenTrueRefuse(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包", Steps: []types.Step{
				{When: `u_费用性质 == 'X'`, Then: "refuse"},
			},
		}},
	}
	e, err := NewEngine(nil, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	form := map[string]interface{}{"u_费用性质": "X"}
	unit := CheckUnit{Label: "明细1"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg == "" {
		t.Error("expected refusal, got empty")
	}
}

func TestRunTarget_WhenFalseSkips(t *testing.T) {
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
	form := map[string]interface{}{"u_费用性质": "Z"}
	unit := CheckUnit{Label: "明细1"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("expected no refusal (when=false, skip), got %q", msg)
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
	// 条件满足 → pass
	form := map[string]interface{}{"u_费用性质": "A", "项目": "P1"}
	unit := CheckUnit{Label: "X"}
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("expected pass, got %q", msg)
	}
	// 条件不满足 → 跳过该 step，无 action，自然通过
	form["项目"] = "P2"
	if msg := e.runTarget(&e.targets[0], unit, form); msg != "" {
		t.Errorf("when=false should skip step, got %q", msg)
	}
}

func TestEvaluate_GlobalSteps(t *testing.T) {
	cfg := &types.RulesConfig{
		GlobalSteps: []types.Step{
			{When: `u_费用性质 == 'A'`, Then: "pass", Action: "免校验"},
		},
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试", Steps: []types.Step{{Action: "match_info_to_budget"}},
		}},
	}
	store := budget.NewStore()
	e, _ := NewEngine(store, nil, cfg)

	// 全局 pass
	action, comment := e.Evaluate(map[string]interface{}{"u_费用性质": "A"}, nil)
	if action != "accept" || comment != "免校验" {
		t.Errorf("expected global pass, got %s/%s", action, comment)
	}

	// 不满足全局条件 → 继续 target 校验（store 为空树，预算包未同步）
	action, comment = e.Evaluate(map[string]interface{}{"u_费用性质": "B"}, nil)
	if action != "refuse" {
		t.Errorf("expected refuse (no store), got %s/%s", action, comment)
	}
}

func TestEvaluate_SplitMode(t *testing.T) {
	cfg := &types.RulesConfig{
		SplitMode: "detail",
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试", Steps: []types.Step{{When: `项目 == 'P1'`, Then: "pass"}},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg)

	// detail 模式：details 里项目=P1 的明细应被 pass
	form := map[string]interface{}{"项目": "PX"}
	details := []map[string]interface{}{
		{"项目": "P1"},
		{"项目": "P2"},
	}
	action, comment := e.Evaluate(form, details)
	// 第一个 detail 项目=P1 → pass；第二个 detail 项目=P2 → 不满足条件，无 action，通过
	// 两个 target 都无拒绝理由
	if action != "accept" {
		t.Errorf("expected accept, got %s/%s", action, comment)
	}
}
