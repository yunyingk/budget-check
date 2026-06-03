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
	Fields     map[string]interface{} // 合并后的所有字段（detail/apportion 覆盖 form）
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
	store       *budget.Store
	client      *ekb.Client
	splitMode   string
	globalSteps []compiledStep
	targets     []compiledTarget
}

func NewEngine(store *budget.Store, client *ekb.Client, cfg *types.RulesConfig) (*Engine, error) {
	e := &Engine{store: store, client: client}
	if cfg == nil {
		return e, nil
	}
	e.splitMode = cfg.SplitMode

	// 编译全局步骤
	for _, s := range cfg.GlobalSteps {
		cs := compiledStep{then: s.Then, action: s.Action}
		if s.When != "" {
			prog, err := expr.Compile(s.When, expr.AllowUndefinedVariables())
			if err != nil {
				return nil, fmt.Errorf("全局步骤表达式编译失败 (%q): %w", s.When, err)
			}
			cs.when = prog
		}
		e.globalSteps = append(e.globalSteps, cs)
	}

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
	if len(e.targets) == 0 && len(e.globalSteps) == 0 {
		return "refuse", "未配置校验规则"
	}

	// 1. 全局前置检查（基于 form 级别字段）
	vars := buildVars(form, nil)
	for _, step := range e.globalSteps {
		if step.when == nil {
			continue
		}
		out, err := expr.Run(step.when, vars)
		if err != nil {
			return "refuse", fmt.Sprintf("全局规则执行失败: %v", err)
		}
		ok, _ := out.(bool)
		if !ok {
			continue
		}
		switch step.then {
		case "pass":
			return "accept", step.action
		case "refuse":
			return "refuse", step.action
		}
	}

	// 2. 提取校验单元
	units := e.extractUnits(form, details)
	if len(units) == 0 {
		return "refuse", "单据无可校验的明细"
	}

	vars = buildVars(form, units)

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

// extractUnits 根据 splitMode 提取校验单元
func (e *Engine) extractUnits(form map[string]interface{}, details []map[string]interface{}) []CheckUnit {
	switch e.splitMode {
	case "detail":
		return extractDetailUnits(form, details, false)
	case "apportion":
		return extractDetailUnits(form, details, true)
	default:
		return extractFormUnit(form)
	}
}

// extractFormUnit 不拆分：form 作为一个整体
func extractFormUnit(form map[string]interface{}) []CheckUnit {
	fields := make(map[string]interface{}, len(form))
	for k, v := range form {
		fields[k] = v
	}
	cc, _ := form["E_system_costcenter"].(string)
	proj, _ := form["项目"].(string)
	return []CheckUnit{{
		CostCenter: cc,
		Project:    proj,
		FeeType:    "",
		Label:      "单据",
		Fields:     fields,
	}}
}

// extractDetailUnits 按明细提取校验单元
// splitApportion=true 时，有分摊的明细会进一步拆分为多条
func extractDetailUnits(form map[string]interface{}, details []map[string]interface{}, splitApportion bool) []CheckUnit {
	formCC, _ := form["E_system_costcenter"].(string)
	formProj, _ := form["项目"].(string)

	if len(details) == 0 {
		return extractFormUnit(form)
	}

	var units []CheckUnit
	for i, detail := range details {
		// 合并：form → detail → feeTypeForm
		fields := make(map[string]interface{}, len(form))
		for k, v := range form {
			fields[k] = v
		}
		for k, v := range detail {
			fields[k] = v
		}

		feeTypeForm, _ := detail["feeTypeForm"].(map[string]interface{})
		if feeTypeForm != nil {
			for k, v := range feeTypeForm {
				fields[k] = v
			}
		}

		feeType := ""
		if feeTypeForm != nil {
			feeType, _ = feeTypeForm["u_费用类型档案"].(string)
		}

		// 从合并后的 fields 取 CostCenter/Project（detail/feeTypeForm 覆盖 form）
		cc := formCC
		proj := formProj
		if v, ok := fields["E_system_costcenter"].(string); ok && v != "" {
			cc = v
		}
		if v, ok := fields["项目"].(string); ok && v != "" {
			proj = v
		}

		if splitApportion {
			var rawApportions []interface{}
			if feeTypeForm != nil {
				rawApportions, _ = feeTypeForm["apportions"].([]interface{})
			}

			if len(rawApportions) > 0 {
				for j, a := range rawApportions {
					apportion, _ := a.(map[string]interface{})
					apportionForm, _ := apportion["apportionForm"].(map[string]interface{})

					// 合并 apportionForm（覆盖）
					apportionFields := make(map[string]interface{}, len(fields))
					for k, v := range fields {
						apportionFields[k] = v
					}
					apportionCC := cc
					apportionProj := proj
					if apportionForm != nil {
						for k, v := range apportionForm {
							apportionFields[k] = v
						}
						if v, ok := apportionForm["E_system_costcenter"].(string); ok && v != "" {
							apportionCC = v
						}
						if v, ok := apportionForm["项目"].(string); ok && v != "" {
							apportionProj = v
						}
					}

					units = append(units, CheckUnit{
						CostCenter: apportionCC,
						Project:    apportionProj,
						FeeType:    feeType,
						Label:      fmt.Sprintf("明细%d分摊%d", i+1, j+1),
						Fields:     apportionFields,
					})
				}
			} else {
				units = append(units, CheckUnit{
					CostCenter: cc,
					Project:    proj,
					FeeType:    feeType,
					Label:      fmt.Sprintf("明细%d", i+1),
					Fields:     fields,
				})
			}
		} else {
			// detail 模式：不拆分摊
			units = append(units, CheckUnit{
				CostCenter: cc,
				Project:    proj,
				FeeType:    feeType,
				Label:      fmt.Sprintf("明细%d", i+1),
				Fields:     fields,
			})
		}
	}

	return units
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
	varsLocal := make(map[string]interface{}, len(vars)+len(unit.Fields)+3)
	for k, v := range vars {
		varsLocal[k] = v
	}
	// unit.Fields 覆盖 form 字段（detail/apportion 覆盖 form）
	for k, v := range unit.Fields {
		varsLocal[k] = v
	}
	// 确保三个关键字段优先使用提取的值
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
