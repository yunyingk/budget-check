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
	if _, err := NewEngine(nil, nil, cfg, nil); err != nil {
		t.Fatalf("compile: %v", err)
	}
}

func TestNewEngine_BadExpr(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "坏规则", Steps: []types.Step{{When: `this is not valid`}},
		}},
	}
	if _, err := NewEngine(nil, nil, cfg, nil); err == nil {
		t.Fatal("expected compile error")
	}
}

func TestNewEngine_NilConfig(t *testing.T) {
	e, err := NewEngine(nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.targets) != 0 {
		t.Fatal("expected empty targets")
	}
}

func TestEvaluate_StepPass(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包",
			Steps: []types.Step{
				{When: `u_费用性质 in ['X','Y']`, Then: "pass"},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg, nil)
	action, comment := e.Evaluate(map[string]interface{}{"u_费用性质": "X"}, nil)
	if action != "accept" {
		t.Errorf("expected accept, got %s/%s", action, comment)
	}
}

func TestEvaluate_StepRefuse(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包",
			Steps: []types.Step{
				{When: `u_费用性质 == 'X'`, Then: "refuse"},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg, nil)
	action, comment := e.Evaluate(map[string]interface{}{"u_费用性质": "X"}, nil)
	if action != "refuse" {
		t.Errorf("expected refuse, got %s/%s", action, comment)
	}
}

func TestEvaluate_StepRefuseWithReason(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包",
			Steps: []types.Step{
				{When: `u_费用性质 == 'X'`, Then: "refuse", Reason: "不符合预算要求"},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg, nil)
	_, comment := e.Evaluate(map[string]interface{}{"u_费用性质": "X"}, nil)
	if comment != "单据 不符合预算要求" {
		t.Errorf("expected reason comment, got %s", comment)
	}
}

func TestEvaluate_WhenFalseSkips(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试包",
			Steps: []types.Step{
				{When: `u_费用性质 == 'X'`},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg, nil)
	action, comment := e.Evaluate(map[string]interface{}{"u_费用性质": "Z"}, nil)
	if action != "accept" {
		t.Errorf("expected accept (when=false skip), got %s/%s", action, comment)
	}
}

func TestEvaluate_SplitDetail(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{{
			ID: "T1", Name: "测试",
			Steps: []types.Step{
				{Action: "split_detail"},
				{When: `项目 == 'P1'`, Then: "pass"},
			},
		}},
	}
	e, _ := NewEngine(nil, nil, cfg, nil)
	form := map[string]interface{}{
		"项目": "PX",
		"details": []interface{}{
			map[string]interface{}{"项目": "P1"},
			map[string]interface{}{"项目": "P2"},
		},
	}
	action, comment := e.Evaluate(form, nil)
	if action != "accept" {
		t.Errorf("expected accept, got %s/%s", action, comment)
	}
}

func TestEvaluate_TwoTargets(t *testing.T) {
	cfg := &types.RulesConfig{
		Targets: []types.RuleTarget{
			{ID: "T1", Name: "A预算", Steps: []types.Step{{Action: "match_info_to_budget"}}},
			{ID: "T2", Name: "B预算", Steps: []types.Step{{Action: "match_info_to_budget"}}},
		},
	}
	store := budget.NewStore()
	e, _ := NewEngine(store, nil, cfg, nil)
	action, comment := e.Evaluate(map[string]interface{}{}, nil)
	if action != "refuse" {
		t.Errorf("expected refuse, got %s/%s", action, comment)
	}
	if comment == "" {
		t.Error("expected refusal comment")
	}
}

func TestSplitDetail_MergesFields(t *testing.T) {
	units := []CheckUnit{{
		Label: "单据",
		Fields: map[string]interface{}{
			"项目":    "FormProject",
			"details": []interface{}{
				map[string]interface{}{
					"项目": "DetailProject",
					"feeTypeForm": map[string]interface{}{
						"u_费用类型档案": "FeeType1",
					},
				},
			},
		},
	}}
	result := splitDetail(units)
	if len(result) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(result))
	}
	if result[0].Fields["项目"] != "DetailProject" {
		t.Errorf("expected detail project to override form, got %v", result[0].Fields["项目"])
	}
	if result[0].Fields["u_费用类型档案"] != "FeeType1" {
		t.Errorf("expected feeTypeForm merged, got %v", result[0].Fields["u_费用类型档案"])
	}
}

func TestSplitApportion_MergesFields(t *testing.T) {
	units := []CheckUnit{{
		Label: "明细1",
		Fields: map[string]interface{}{
			"项目": "Original",
			"apportions": []interface{}{
				map[string]interface{}{
					"apportionForm": map[string]interface{}{
						"项目": "Apportion1",
					},
				},
			},
		},
	}}
	result := splitApportion(units)
	if len(result) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(result))
	}
	if result[0].Fields["项目"] != "Apportion1" {
		t.Errorf("expected apportionForm to override, got %v", result[0].Fields["项目"])
	}
	if _, ok := result[0].Fields["apportions"]; ok {
		t.Error("apportions should be deleted after split")
	}
}
