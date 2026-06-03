package rules

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/types"
	"fmt"
	"log"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// CheckUnit 校验单元（一条记录），所有字段放在 Fields 中动态存取
type CheckUnit struct {
	Label     string
	Fields    map[string]interface{}
	Committed bool // true 表示已提交，后续非-split steps 跳过
}

// compiledStep 预编译后的 step，when 表达式在加载阶段编译为字节码
type compiledStep struct {
	when        *vm.Program
	then        string
	action      string
	reason      string
	description string
}

// compiledTarget 预编译后的 target
type compiledTarget struct {
	def    types.RuleTarget
	steps  []compiledStep
	dimMap map[string]string // dimType -> fieldName
}

// Engine 规则引擎
type Engine struct {
	store   *budget.Store
	client  *ekb.Client
	targets []compiledTarget
}

func NewEngine(store *budget.Store, client *ekb.Client, cfg *types.RulesConfig, dimMap map[string]string) (*Engine, error) {
	e := &Engine{store: store, client: client}
	if cfg == nil {
		return e, nil
	}
	// dimension_map：dimType -> 表单字段名，外部传入覆盖默认
	merged := map[string]string{
		"costCenter": "E_system_costcenter",
		"project":    "项目",
		"feeType":    "u_费用类型档案",
	}
	for k, v := range dimMap {
		merged[k] = v
	}
	for _, t := range cfg.Targets {
		ct := compiledTarget{def: t, dimMap: merged}
		for _, s := range t.Steps {
			cs, err := compileStep(s, t.Name)
			if err != nil {
				return nil, err
			}
			ct.steps = append(ct.steps, cs)
		}
		e.targets = append(e.targets, ct)
	}
	return e, nil
}

func compileStep(s types.Step, targetName string) (compiledStep, error) {
	cs := compiledStep{then: s.Then, action: s.Action, reason: s.Reason, description: s.Description}
	if s.When != "" {
		prog, err := expr.Compile(s.When, expr.AllowUndefinedVariables())
		if err != nil {
			return cs, fmt.Errorf("target %s 表达式编译失败 (%q): %w", targetName, s.When, err)
		}
		cs.when = prog
	}
	return cs, nil
}

// Evaluate 对单据执行规则校验
func (e *Engine) Evaluate(form map[string]interface{}) (string, string) {
	if len(e.targets) == 0 {
		return "refuse", "未配置校验规则"
	}
	var refusals []string
	for _, target := range e.targets {
		if msg := e.runTargetWorkflow(&target, form); msg != "" {
			refusals = append(refusals, msg)
		}
	}
	if len(refusals) > 0 {
		return "refuse", joinStrings(refusals, "；")
	}
	return "accept", "同意"
}

// runTargetWorkflow 执行单个 target 的完整工作流：steps 顺序执行
func (e *Engine) runTargetWorkflow(target *compiledTarget, form map[string]interface{}) string {
	// 初始数据集：form 作为一条记录
	units := []CheckUnit{{Label: "单据", Fields: shallowCopy(form)}}

	// 顺序执行 steps
	for _, step := range target.steps {
		switch step.action {
		case "split_detail":
			units = splitDetail(units)
		case "split_apportion":
			units = splitApportion(units)
		default:
			var remaining []CheckUnit
			for _, unit := range units {
				if unit.Committed {
					remaining = append(remaining, unit)
					continue
				}
				msg := e.runStep(target, step, unit, form)
				if msg == "__PASS__" {
					continue // 该 unit 已通过，不再执行后续 steps
				}
				if msg == "__COMMIT__" {
					unit.Committed = true
					remaining = append(remaining, unit)
					continue // 保留该 unit，后续非-split steps 跳过
				}
				if msg != "" {
					return msg
				}
				remaining = append(remaining, unit)
			}
			units = remaining
		}
	}
	return ""
}

// runStep 对单个 unit 执行一个 step
// 返回空字符串 = 继续执行；"__PASS__" = unit 通过；"__COMMIT__" = unit 提交；其他 = 拒绝消息
func (e *Engine) runStep(target *compiledTarget, step compiledStep, unit CheckUnit, form map[string]interface{}) string {
	vars := make(map[string]interface{}, len(form)+len(unit.Fields))
	for k, v := range form {
		vars[k] = v
	}
	for k, v := range unit.Fields {
		vars[k] = v
	}

	if step.when != nil {
		out, err := expr.Run(step.when, vars)
		if err != nil {
			return fmt.Sprintf("%s 规则执行失败: %v", target.def.Name, err)
		}
		ok, _ := out.(bool)
		if !ok {
			return "" // 条件不满足，跳过该 step
		}
	}

	switch step.then {
	case "pass":
		if step.reason != "" {
			log.Printf("[Engine] %s %s: %s", target.def.Name, unit.Label, step.reason)
		}
		return "__PASS__"
	case "commit":
		if step.reason != "" {
			log.Printf("[Engine] %s %s committed: %s", target.def.Name, unit.Label, step.reason)
		}
		return "__COMMIT__"
	case "refuse":
		if step.reason != "" {
			return fmt.Sprintf("%s %s", unit.Label, step.reason)
		}
		return fmt.Sprintf("%s %s", unit.Label, target.def.Name)
	}

	switch step.action {
	case "match_info_to_budget":
		if msg := e.matchToBudget(target, unit); msg != "" {
			return fmt.Sprintf("%s %s", unit.Label, msg)
		}
	}
	return ""
}

// splitDetail 按 details 拆分：form + detail 字段 + feeTypeForm 扁平化合并
func splitDetail(units []CheckUnit) []CheckUnit {
	var result []CheckUnit
	for _, unit := range units {
		if unit.Committed {
			result = append(result, unit)
			continue
		}
		rawDetails, ok := unit.Fields["details"].([]interface{})
		if !ok || len(rawDetails) == 0 {
			result = append(result, unit)
			continue
		}
		for i, d := range rawDetails {
			detail, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			fields := shallowCopy(unit.Fields)
			delete(fields, "details") // 避免递归
			for k, v := range detail {
				if k == "feeTypeForm" {
					if feeTypeForm, ok := v.(map[string]interface{}); ok {
						for fk, fv := range feeTypeForm {
							fields[fk] = fv
						}
					}
				} else {
					fields[k] = v
				}
			}
			result = append(result, CheckUnit{
				Label:  fmt.Sprintf("明细%d", i+1),
				Fields: fields,
			})
		}
	}
	return result
}

// splitApportion 按 apportions 拆分：当前字段 + apportionForm 合并
func splitApportion(units []CheckUnit) []CheckUnit {
	var result []CheckUnit
	for _, unit := range units {
		if unit.Committed {
			result = append(result, unit)
			continue
		}
		rawApportions, ok := unit.Fields["apportions"].([]interface{})
		if !ok || len(rawApportions) == 0 {
			result = append(result, unit)
			continue
		}
		for i, a := range rawApportions {
			apportion, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			fields := shallowCopy(unit.Fields)
			delete(fields, "apportions")
			if apportionForm, ok := apportion["apportionForm"].(map[string]interface{}); ok {
				for k, v := range apportionForm {
					fields[k] = v
				}
			}
			result = append(result, CheckUnit{
				Label:  fmt.Sprintf("%s分摊%d", unit.Label, i+1),
				Fields: fields,
			})
		}
	}
	return result
}

// matchToBudget 把单据字段匹配到预算树
func (e *Engine) matchToBudget(target *compiledTarget, unit CheckUnit) string {
	tree := e.store.GetTreeByID(target.def.ID)
	if tree == nil {
		return "预算包未同步"
	}
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
		fieldName, ok := target.dimMap[n.DimType]
		if !ok {
			continue
		}
		fieldValue, _ := unit.Fields[fieldName].(string)
		if fieldValue == "" {
			return fmt.Sprintf("缺少%s", fieldName)
		}
		id, found := e.client.FindAncestorInTree(fieldValue, rootSet, 5)
		if !found {
			return fmt.Sprintf("%s %s 不在预算包内", fieldName, fieldValue)
		}
		rootNode = tree.Root[id]
		rootFieldName = fieldName
		break
	}

	if rootNode == nil {
		return "预算包根维度未配置"
	}

	feeTypeField, ok := target.dimMap["feeType"]
	if !ok {
		return ""
	}
	feeType, _ := unit.Fields[feeTypeField].(string)
	if feeType == "" {
		return ""
	}

	feeSet := collectFeeTypes(rootNode)
	if len(feeSet) == 0 {
		return ""
	}
	if _, found := e.client.FindAncestorInTree(feeType, feeSet, 5); !found {
		return fmt.Sprintf("费用类型 %s 不在%s预算包内", feeType, rootFieldName)
	}
	return ""
}

func collectFeeTypes(root *budget.Node) map[string]bool {
	out := make(map[string]bool)
	for _, child := range root.Children {
		for k := range child.Children {
			out[k] = true
		}
	}
	return out
}

func shallowCopy(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
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
