# v0.8.0 预算包配置化设计方案

## 目标

核心代码不包含任何业务含义字符串（"业务""管理""生产"等），所有路由和校验逻辑由配置驱动，新增预算包场景零代码改动。

---

## 配置结构

```yaml
# 费用性质 ID→显示名，只用于错误消息拼中文
expense_natures:
  ID01LPD78hZRsr: "业务"
  ID01LPDisfN3qv: "管理"
  ID01LPDfjPcnyn: "生产"

# 预算包：每个包一个 condition + checks 序列
budget_targets:
  - id: "ID01T5nPu92vH9"
    name: "成本中心预算"
    depth: 99
    condition: "expenseNature in ['ID01LPD78hZRsr','ID01LPDisfN3qv']"
    checks:
      - field: "costCenter"
        mode: "required"
        error: "{label} 缺少成本中心"

      - field: "costCenter"
        mode: "ancestor_in_root"
        error: "{label} 成本中心 {fieldName}({fieldValue}) 不在{budgetName}内"

      - field: "feeType"
        mode: "descendant_of"
        match_from: "costCenter"
        skip_if_empty: true
        error: "{label} 费用类型 {fieldName}({fieldValue}) 不在{budgetName}内"

  - id: "ID01T5kHipEY7J"
    name: "项目预算"
    depth: 99
    condition: "expenseNature == 'ID01LPDfjPcnyn'"
    checks:
      - field: "project"
        mode: "required"
        error: "{label} 生产费用缺少项目"

      - field: "project"
        mode: "ancestor_in_root"
        error: "{label} 项目 {fieldName}({fieldValue}) 不在{budgetName}内"

      - field: "costCenter"
        mode: "descendant_of"
        match_from: "project"
        skip_if_leaf: true
        error: "{label} 成本中心 {fieldName}({fieldValue}) 不在{budgetName}内"
```

---

## 四种校验模式

| mode | 含义 | 额外参数 |
|---|---|---|
| `required` | 字段不能为空 | — |
| `ancestor_in_root` | 字段值是某根节点的后代，匹配结果供后续步骤引用 | — |
| `descendant_of` | 字段值在 `match_from` 字段匹配节点的子树内 | `match_from` |
| `not_in_exempt` | 字段值在豁免列表 → 跳过本包剩余步骤，本包直接 accept | — |

### 模式详解

- **required**: 检查 `field` 是否为空字符串，空则 refuse
- **ancestor_in_root**: 在当前预算包树的根节点集合中，查找 `field` 值属于哪个根节点的后代（调用 `FindAncestorInTree`），匹配结果记住供后续 `descendant_of` 引用
- **descendant_of**: 先取出 `match_from` 字段之前 `ancestor_in_root` 的匹配节点，检查 `field` 值是否在该匹配节点的子树内（子节点及其所有后代）
- **not_in_exempt**: 保留以备特殊豁免场景；当前推荐用 condition 表达式替代

---

## 条件表达式

- **可用字段**: `expenseNature`（原始 ID）、`costCenter`、`project`、`feeType`
- **运算符**: `==` `!=` `in` `not in` `&&` `||` `!` `()`
- **示例**:
  - `expenseNature in ['ID01LPD78hZRsr','ID01LPDisfN3qv']`
  - `expenseNature == 'ID01LPDfjPcnyn'`
  - `expenseNature == 'ID01LPDfjPcnyn' && project != 'ID01LZNNxip807'`
- **condition 不填** = 始终命中（向后兼容）

---

## 判定流程

```
对每个 checkUnit:
  遍历 budget_targets, 对 condition 求值

  命中0个包 → refuse "未命中任何预算包"
  命中1+个包 → 逐包顺序执行 checks
    required 检查不通过 → 该包失败
    ancestor_in_root 未找到 → 该包失败
    descendant_of 未找到 → 该包失败
    skip_if_empty 且字段为空 → 跳过当前步骤
    skip_if_leaf 且匹配节点无子节点 → 跳过当前步骤

  全部命中包都通过 → accept
  任一命中包失败 → refuse（分号拼接所有原因）
```

---

## 豁免项目

去掉 `exempt_projects` 硬编码，改为条件表达式：

```yaml
# 项目包 condition 加排除
condition: "expenseNature == 'ID01LPDfjPcnyn' && project != 'ID01LZNNxip807'"

# 成本中心包 condition 加包含豁免项目场景
# 豁免项目不会命中项目包，自然走成本中心包
```

不再需要 `exempt_projects` 配置项。

---

## 错误消息模板变量

| 变量 | 值 |
|---|---|
| `{label}` | checkUnit 标签（"单据"/"明细1"/"明细1分摊2"） |
| `{fieldName}` | 字段的维度中文名（从 `GetDimension` 取） |
| `{fieldValue}` | 字段的原始 ID |
| `{budgetName}` | 当前预算包 name |

---

## 删除的硬编码

| 删除项 | 替代方式 |
|---|---|
| `ExpenseNature` map | `expense_natures` 配置 |
| `switch natureName` 路由 | `condition` 表达式 |
| `checkBusinessUnit()` | checks 序列配置 |
| `checkProductionUnit()` | checks 序列配置 |
| `ExemptProjects` 字段 | `condition` 里 `project !=` |

---

## 当前代码等价性

| 当前硬编码 | 新配置 |
|---|---|
| `ExpenseNature` map + `switch natureName` | `expense_natures` + `condition` 表达式 |
| "业务/管理" → `checkBusinessUnit` | 成本中心包 condition + 3 步 checks |
| "生产" → `checkProductionUnit` + 末尾调 `checkBusinessUnit` | 项目包 condition + checks & 成本中心包 condition 也包含"生产" |
| `exempt_projects` 特殊分支 | 项目包 condition 加 `project != 'ID01LZNNxip807'` |

关键等价：当前"生产"场景**同时命中两个包**（项目包 + 成本中心包），两个包各自用自己的树校验，全部通过才 accept。这和当前 `checkProductionUnit` 末尾调 `checkBusinessUnit` 完全等价。

---

## 新增场景示例

客户加"研发预算"——只加配置，不改代码：

```yaml
expense_natures:
  # ... 原有三个 ...
  ID01R&D_NATURE: "研发"

budget_targets:
  # ... 原有两个包 ...

  - id: "ID01R&D_BUDGET"
    name: "研发预算"
    depth: 99
    condition: "expenseNature == 'ID01R&D_NATURE'"
    checks:
      - field: "project"
        mode: "ancestor_in_root"
        error: "{label} 项目不在研发预算包内"
      - field: "feeType"
        mode: "descendant_of"
        match_from: "project"
        error: "{label} 费用类型不在研发预算包内"
```