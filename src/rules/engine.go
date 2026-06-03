package rules

import (
	"budget/src/budget"
	"budget/src/types"
	"fmt"
	"strings"
)

type CheckUnit struct {
	CostCenter string
	Project    string
	FeeType    string
	Label      string
}

type Engine struct {
	store *budget.Store
	rules *types.RulesConfig
}

func NewEngine(store *budget.Store, rules *types.RulesConfig) *Engine {
	return &Engine{
		store: store,
		rules: rules,
	}
}

func (e *Engine) Evaluate(form map[string]interface{}, details []map[string]interface{}) (string, string) {
	units := extractCheckUnits(form, details)
	
	var refusals []string
	for _, unit := range units {
		action, comment := e.evaluateUnit(unit, form)
		if action == "refuse" {
			refusals = append(refusals, comment)
		}
	}
	
	if len(refusals) > 0 {
		return "refuse", joinStrings(refusals, "；")
	}
	return "accept", "同意"
}

func (e *Engine) evaluateUnit(unit CheckUnit, form map[string]interface{}) (string, string) {
	for _, target := range e.rules.Targets {
		action, comment := e.evaluateTarget(target, unit, form)
		if action == "refuse" {
			return action, comment
		}
	}
	return "accept", "同意"
}

func (e *Engine) evaluateTarget(target types.RuleTarget, unit CheckUnit, form map[string]interface{}) (string, string) {
	vars := map[string]interface{}{
		"expenseNature": form["u_费用性质"],
		"costCenter":    unit.CostCenter,
		"project":       unit.Project,
		"feeType":       unit.FeeType,
	}
	
	for _, step := range target.Steps {
		if step.When != "" {
			result := evaluateExpression(step.When, vars)
			if !result {
				if step.Then == "pass" {
					return "accept", ""
				}
				continue
			}
		}
		
		if step.Action == "make_info_to_detail" {
			continue
		}
		
		if step.Action == "match_info_to_budget" {
			tree := e.store.GetTreeByID(target.ID)
			if tree == nil {
				return "refuse", fmt.Sprintf("%s 预算包未同步", target.Name)
			}
			
			matched, comment := matchToBudget(tree, unit)
			if !matched {
				return "refuse", comment
			}
		}
	}
	
	return "accept", ""
}

func matchToBudget(tree *budget.Tree, unit CheckUnit) (bool, string) {
	for _, rootNode := range tree.Root {
		if rootNode.DimType == "costCenter" || rootNode.DimType == "project" {
			fieldValue := getFieldValueByDimType(unit, rootNode.DimType)
			if fieldValue == "" {
				return false, fmt.Sprintf("缺少字段 %s", rootNode.DimType)
			}
			
			if _, found := tree.Root[fieldValue]; !found {
				return false, fmt.Sprintf("%s 不在预算包内", fieldValue)
			}
			
			for _, childNode := range rootNode.Children {
				if childNode.DimType == "feeType" {
					if unit.FeeType == "" {
						continue
					}
					if _, found := childNode.Children[unit.FeeType]; !found {
						return false, fmt.Sprintf("费用类型 %s 不在预算包内", unit.FeeType)
					}
				}
			}
		}
	}
	return true, ""
}

func getFieldValueByDimType(unit CheckUnit, dimType string) string {
	switch dimType {
	case "costCenter":
		return unit.CostCenter
	case "project":
		return unit.Project
	case "feeType":
		return unit.FeeType
	}
	return ""
}

func evaluateExpression(expr string, vars map[string]interface{}) bool {
	expr = strings.TrimSpace(expr)
	
	if strings.Contains(expr, "not in") {
		parts := strings.SplitN(expr, "not in", 2)
		if len(parts) == 2 {
			field := strings.TrimSpace(parts[0])
			listStr := strings.TrimSpace(parts[1])
			listStr = strings.Trim(listStr, "[]")
			listStr = strings.ReplaceAll(listStr, "'", "")
			items := strings.Split(listStr, ",")
			
			value, ok := vars[field]
			if !ok {
				return false
			}
			valueStr, ok := value.(string)
			if !ok {
				return false
			}
			
			for _, item := range items {
				if strings.TrimSpace(item) == valueStr {
					return false
				}
			}
			return true
		}
	}
	
	if strings.Contains(expr, "!=") {
		parts := strings.SplitN(expr, "!=", 2)
		if len(parts) == 2 {
			field := strings.TrimSpace(parts[0])
			expected := strings.TrimSpace(parts[1])
			expected = strings.Trim(expected, "'")
			
			value, ok := vars[field]
			if !ok {
				return true
			}
			valueStr, ok := value.(string)
			if !ok {
				return true
			}
			return valueStr != expected
		}
	}
	
	return false
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

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
