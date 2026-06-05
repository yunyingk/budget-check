# 工作流语法（AI Agent 版）

## Schema

```typescript
interface RulesConfig {
  version: 1;
  targets: Target[];
}

interface Target {
  id: string;           // 预算包 ID
  name: string;         // 预算包名称
  steps: Step[];        // 步骤数组，顺序执行
}

type Step = ActionStep | WhenStep;

interface ActionStep {
  description: string;
  action: ActionType;
}

interface WhenStep {
  description: string;
  when: string;         // expr-lang 表达式
  then: ThenAction;
  reason?: string;      // refuse 时的原因
}

type ActionType = "split_detail" | "split_apportion" | "match_info_to_budget";
type ThenAction = "pass" | "refuse" | "commit";
```

---

## Actions

### split_detail

- **输入**: `CheckUnit[]`
- **输出**: `CheckUnit[]`
- **逻辑**:
  - 遍历每个 unit
  - 如果 `unit.Committed == true`，原样保留
  - 如果 `unit.Fields["details"]` 不存在或为空，原样保留
  - 否则，遍历 `details` 数组：
    - `newFields = shallowCopy(unit.Fields)`
    - `delete(newFields, "details")`
    - 对于 detail 中的每个字段：
      - 如果 key == `"feeTypeForm"` 且 value 是 map，展开到顶层
      - 否则直接复制
    - 创建新 `CheckUnit`，Label = `"明细{i+1}"`
- **副作用**: 无

### split_apportion

- **输入**: `CheckUnit[]`
- **输出**: `CheckUnit[]`
- **逻辑**:
  - 遍历每个 unit
  - 如果 `unit.Committed == true`，原样保留
  - 如果 `unit.Fields["apportions"]` 不存在或为空，原样保留
  - 否则，遍历 `apportions` 数组：
    - `newFields = shallowCopy(unit.Fields)`
    - `delete(newFields, "apportions")`
    - 如果 `apportion["apportionForm"]` 存在且是 map，展开到顶层
    - 创建新 `CheckUnit`，Label = `"{原Label}分摊{i+1}"`
- **副作用**: 无

### match_info_to_budget

- **输入**: `CheckUnit`
- **输出**: `string`（空字符串 = 通过，非空 = 拒绝原因）
- **逻辑**:
  1. 获取预算树 `tree = store.GetTreeByID(target.id)`
  2. 如果 tree 为空，返回 `"预算包未同步"`
  3. 如果 tree.Root 为空，返回 `"预算包为空"`
  4. `currentNodes = tree.Root`
  5. 循环直到 `currentNodes` 为空：
     - 取第一个节点 `first`
     - `fieldValue = unit.Fields[first.DimId]`
     - 如果 fieldValue 为空，返回 `"缺少{first.DimId}"`
     - 构建当前层节点集合 `set`
     - 如果 `first.DimType == "PROJECT"`：
       - `id, found = client.FindProjectAncestorInTree(fieldValue, set, 5)`
       - 如果 !found，返回 `"{first.DimId} {fieldValue} 不在预算包内"`
       - `matched = currentNodes[id]`
     - 如果 `first.DimType == "DEPART"`：
       - `id, found = client.FindDepartmentAncestorInTree(fieldValue, set, 5)`
       - 如果 !found，返回 `"{first.DimId} {fieldValue} 不在预算包内"`
       - `matched = currentNodes[id]`
     - 如果 `first.DimType == "FEE_TYPE"`：
       - `id, found = client.FindFeeTypeAncestorInTree(fieldValue, set, 5)`
       - 如果 !found，返回 `"{first.DimId} {fieldValue} 不在预算包内"`
       - `matched = currentNodes[id]`
     - 否则：
       - 如果 `currentNodes[fieldValue]` 存在，`matched = 该节点`
       - 否则，返回 `"{first.DimId} {fieldValue} 不在预算包内"`
     - `currentNodes = matched.Children`
  6. 返回 `""`

---

## WhenStep 语义

### when 表达式

- **语法**: [expr-lang](https://github.com/expr-lang/expr)
- **输入变量**: 原始 `form` 字段 + 当前 `unit.Fields` 字段；同名时 `unit.Fields` 覆盖 `form`
- **返回类型**: `bool`
- **编译时**: 启动时编译为 `vm.Program`
- **运行时**: `expr.Run(program, vars)`

### then 动作

| then | 返回值 | 处理 |
|------|--------|------|
| `pass` | `"__PASS__"` | 从 `units` 中移除该 unit |
| `commit` | `"__COMMIT__"` | 设置 `unit.Committed = true`，保留在 `units` 中 |
| `refuse` | 拒绝消息 | 返回拒绝消息，终止整个 target |

---

## 数据流

```
初始: units = [{ Label: "单据", Fields: shallowCopy(form) }]

for step in target.steps:
  switch step.action:
    case "split_detail":
      units = splitDetail(units)
    case "split_apportion":
      units = splitApportion(units)
    default:
      // when/then 或 match_info_to_budget
      remaining = []
      for unit in units:
        if unit.Committed:
          remaining.append(unit)
          continue

        msg = runStep(step, unit)

        if msg == "__PASS__":
          continue  // 不加入 remaining
        if msg == "__COMMIT__":
          unit.Committed = true
          remaining.append(unit)
          continue
        if msg != "":
          return msg  // 拒绝
        remaining.append(unit)

      units = remaining

return ""  // 通过
```

---

## CheckUnit

```typescript
interface CheckUnit {
  Label: string;                    // 标签，如 "单据"、"明细1"、"明细1分摊2"
  Fields: Record<string, any>;      // 字段 map，动态存取
  Committed: boolean;               // 是否已提交
}
```

---

## 状态转换

```
unit 状态:
  - 正常: Committed = false
  - 已提交: Committed = true

状态转换:
  - 正常 → pass: 从 units 中移除
  - 正常 → commit: Committed = true
  - 正常 → refuse: 终止整个 target
  - 已提交: 跳过所有非-split steps
```

---

## 预算树结构

```typescript
interface Tree {
  ID: string;
  Root: Record<string, Node>;  // dimCode → Node
}

interface Node {
  DimCode: string;
  DimType: string;             // "PROJECT" | "DEPART" | "STAFF" | ...
  DimId: string;               // 表单字段名
  NodeName: string;
  NodeID: string;
  IsLeaf: boolean;
  Children: Record<string, Node>;
}
```

### 匹配算法

```
function matchToBudget(unit, tree):
  currentNodes = tree.Root

  while currentNodes is not empty:
    first = firstValue(currentNodes)
    fieldValue = unit.Fields[first.DimId]

    if fieldValue is empty:
      return "缺少{first.DimId}"

    set = keys(currentNodes)

    if first.DimType == "PROJECT":
      id, found = client.FindProjectAncestorInTree(fieldValue, set, 5)
      if not found:
        return "{first.DimId} {fieldValue} 不在预算包内"
      matched = currentNodes[id]
    else if first.DimType == "DEPART":
      id, found = client.FindDepartmentAncestorInTree(fieldValue, set, 5)
      if not found:
        return "{first.DimId} {fieldValue} 不在预算包内"
      matched = currentNodes[id]
    else if first.DimType == "FEE_TYPE":
      id, found = client.FindFeeTypeAncestorInTree(fieldValue, set, 5)
      if not found:
        return "{first.DimId} {fieldValue} 不在预算包内"
      matched = currentNodes[id]
    else:
      if fieldValue in currentNodes:
        matched = currentNodes[fieldValue]
      else:
        return "{first.DimId} {fieldValue} 不在预算包内"

    currentNodes = matched.Children

  return ""
```

---

## 示例配置

```json
{
  "version": 1,
  "targets": [
    {
      "id": "ID01TsPQJFK1RR",
      "name": "成本中心预算",
      "steps": [
        { "description": "按费用明细拆分", "action": "split_detail" },
        { "description": "启动分摊", "action": "split_apportion" },
        { "description": "匹配成本中心预算包", "action": "match_info_to_budget" }
      ]
    },
    {
      "id": "ID01T5kHipEY7J",
      "name": "项目预算",
      "steps": [
        {
          "description": "费用性质非生产时免项目预算校验",
          "when": "u_费用性质 != 'ID01LPDfjPcnyn'",
          "then": "pass"
        },
        { "description": "按费用明细拆分", "action": "split_detail" },
        {
          "description": "指定项目免预算校验",
          "when": "项目 == 'ID01LZNNxip807'",
          "then": "pass"
        },
        { "description": "启动分摊", "action": "split_apportion" },
        { "description": "匹配项目预算包", "action": "match_info_to_budget" }
      ]
    }
  ]
}
```

---

## 约束

1. `split_apportion` 必须在 `split_detail` 之后（apportions 嵌套在 details 里）
2. 连续两个 `split_apportion` 无效（第二个会跳过）
3. `when` 表达式可用字段包括所有已合并的字段（form + detail + apportionForm）
4. 每个 target 独立执行，任一拒绝则整体拒绝
