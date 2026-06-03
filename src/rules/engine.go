package rules

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/types"
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// CheckUnit 校验单元（一条明细或一个分摊）
type CheckUnit struct {
	CostCenter string
	Project    string
	FeeType    string
	Label      string
}

// compiledStep 预编译后的 step，when 表达式在加载阶段编译为字节码
type compiledStep struct {
	when   *vm.Program
	then   string
	action string
}

// compiledTarget 预编译后的 target
type compiledTarget struct {
	def   types.RuleTarget
	steps []compiledStep
}

// Engine 规则引擎
type Engine struct {
	store   *budget.Store
	client  *ekb.Client
	targets []compiledTarget
}

func NewEngine(store *budget.Store, client *ekb.Client, cfg *types.RulesConfig) (*Engine, error) {
	if cfg == nil {
		return &Engine{store: store, client: client}, nil
	}
	e := &Engine{store: store, client: client}
	for _, t := range cfg.Targets {
		ct := compiledTarget{def: t}
		for _, s := range t.Steps {
			cs := compiledStep{then: s.Then, action: s.Action}
			if s.When != "" {
				prog, err := expr.Compile(s.When, expr.AllowUndefinedVariables())
				if err != nil {
					return nil, fmt.Errorf("target %s 表达式编译失败 (%q): %w", t.Name, s.When, err)
				}
				cs.when = prog
			}
			ct.steps = append(ct.steps, cs)
		}
		e.targets = append(e.targets, ct)
	}
	return e, nil
}

// Evaluate 对单据执行规则校验
func (e *Engine) Evaluate(form map[string]interface{}, details []map[string]interface{}) (string, string) {
	if len(e.targets) == 0 {
		return "refuse", "未配置校验规则"
	}

	units := extractCheckUnits(form, details)
	if len(units) == 0 {
		return "refuse", "单据无可校验的明细"
	}

	vars := buildVars(form, units)

	var refusals []string
	for _, unit := range units {
		for _, t := range e.targets {
			msg := e.runTarget(&t, unit, vars)
			if msg != "" {
				refusals = append(refusals, msg)
			}
		}
	}

	if len(refusals) > 0 {
		return "refuse", joinStrings(refusals, "；")
	}
	return "accept", "同意"
}

// buildVars 把 form 字段全部摊平进 vars，rule 直接按字段名引用
func buildVars(form map[string]interface{}, units []CheckUnit) map[string]interface{} {
	out := make(map[string]interface{}, len(form))
	for k, v := range form {
		out[k] = v
	}
	if len(units) > 0 {
		out["E_system_costcenter"] = units[0].CostCenter
		out["项目"] = units[0].Project
	}
	return out
}

// runTarget 跑一个 target 的全部 step
func (e *Engine) runTarget(target *compiledTarget, unit CheckUnit, vars map[string]interface{}) string {
	varsLocal := make(map[string]interface{}, len(vars)+3)
	for k, v := range vars {
		varsLocal[k] = v
	}
	if unit.CostCenter != "" {
		varsLocal["E_system_costcenter"] = unit.CostCenter
	}
	if unit.Project != "" {
		varsLocal["项目"] = unit.Project
	}
	if unit.FeeType != "" {
		varsLocal["u_费用类型档案"] = unit.FeeType
	}

	var tree *budget.Tree

	for _, step := range target.steps {
		if step.when != nil {
			out, err := expr.Run(step.when, varsLocal)
			if err != nil {
				return fmt.Sprintf("%s 规则执行失败: %s", target.def.Name, err)
			}
			ok, _ := out.(bool)
			if !ok {
				if step.then == "pass" {
					return ""
				}
				continue
			}
			if step.then == "refuse" {
				return fmt.Sprintf("%s %s", unit.Label, target.def.Name)
			}
		}

		switch step.action {
		case "make_info_to_detail":
			// 暂为占位动作
		case "match_info_to_budget":
			if tree == nil {
				tree = e.store.GetTreeByID(target.def.ID)
				if tree == nil {
					return fmt.Sprintf("%s 预算包 %s 未同步", unit.Label, target.def.Name)
				}
			}
			if msg := matchToBudget(e.client, tree, unit); msg != "" {
				return fmt.Sprintf("%s %s", unit.Label, msg)
			}
		}
	}
	return ""
}

// matchToBudget 把单据字段匹配到预算树
//
// 树的 root 维度类型决定匹配路径：rootNode.DimType == "costCenter" 时
// 走 costCenter → feeType 路径；"project" 时走 project → (children) → costCenter
// → feeType 路径。匹配时沿父级链向上查找（FindAncestorInTree）。
func matchToBudget(client *ekb.Client, tree *budget.Tree, unit CheckUnit) string {
	if len(tree.Root) == 0 {
		return "预算包为空"
	}

	rootSet := make(map[string]bool, len(tree.Root))
	for k := range tree.Root {
		rootSet[k] = true
	}

	var rootNode *budget.Node
	var rootFieldName string

	for _, n := range tree.Root {
		switch n.DimType {
		case "costCenter":
			if unit.CostCenter == "" {
				return "缺少成本中心"
			}
			id, ok := client.FindAncestorInTree(unit.CostCenter, rootSet, 5)
			if !ok {
				return fmt.Sprintf("成本中心 %s 不在预算包内", unit.CostCenter)
			}
			rootNode = tree.Root[id]
			rootFieldName = "成本中心"
		case "project":
			if unit.Project == "" {
				return "缺少项目"
			}
			id, ok := client.FindAncestorInTree(unit.Project, rootSet, 5)
			if !ok {
				return fmt.Sprintf("项目 %s 不在预算包内", unit.Project)
			}
			rootNode = tree.Root[id]
			rootFieldName = "项目"
		default:
			continue
		}
		break
	}

	if rootNode == nil {
		return "预算包根维度类型未知"
	}

	if unit.FeeType == "" {
		return ""
	}

	feeSet := collectFeeTypes(rootNode)
	if len(feeSet) == 0 {
		return ""
	}
	if _, ok := client.FindAncestorInTree(unit.FeeType, feeSet, 5); !ok {
		return fmt.Sprintf("费用类型 %s 不在%s预算包内", unit.FeeType, rootFieldName)
	}
	return ""
}

// collectFeeTypes 收集根节点下所有费用类型（含子节点）
func collectFeeTypes(root *budget.Node) map[string]bool {
	out := make(map[string]bool)
	for _, child := range root.Children {
		for k := range child.Children {
			out[k] = true
		}
	}
	return out
}

func extractCheckUnits(form map[string]interface{}, details []map[string]interface{}) []CheckUnit {
	formCostCenter, _ := form["E_system_costcenter"].(string)
	formProject, _ := form["项目"].(string)

	if len(details) == 0 {
		return []CheckUnit{{
			CostCenter: formCostCenter,
			Project:    formProject,
			FeeType:    "",
			Label:      "单据",
		}}
	}

	var units []CheckUnit
	for i, detail := range details {
		feeTypeForm, _ := detail["feeTypeForm"].(map[string]interface{})
		feeType := ""
		if feeTypeForm != nil {
			feeType, _ = feeTypeForm["u_费用类型档案"].(string)
		}

		var rawApportions []interface{}
		if feeTypeForm != nil {
			rawApportions, _ = feeTypeForm["apportions"].([]interface{})
		}

		if len(rawApportions) > 0 {
			for j, a := range rawApportions {
				apportion, _ := a.(map[string]interface{})
				apportionForm, _ := apportion["apportionForm"].(map[string]interface{})

				cc := formCostCenter
				proj := formProject
				if apportionForm != nil {
					if v, ok := apportionForm["E_system_costcenter"].(string); ok && v != "" {
						cc = v
					}
					if v, ok := apportionForm["项目"].(string); ok && v != "" {
						proj = v
					}
				}

				units = append(units, CheckUnit{
					CostCenter: cc,
					Project:    proj,
					FeeType:    feeType,
					Label:      fmt.Sprintf("明细%d分摊%d", i+1, j+1),
				})
			}
		} else {
			units = append(units, CheckUnit{
				CostCenter: formCostCenter,
				Project:    formProject,
				FeeType:    feeType,
				Label:      fmt.Sprintf("明细%d", i+1),
			})
		}
	}

	return units
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, s := range parts[1:] {
		out += sep + s
	}
	return out
}
