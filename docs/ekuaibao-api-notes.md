# 合思（易快报）API 备忘

## 坑点记录

### 1. API 路径前缀

所有接口路径需要加 `/api/openapi` 前缀，文档里写的路径经常省略这个前缀。

**错误:** `https://app.ekuaibao.com/v2/budgets/...`
**正确:** `https://app.ekuaibao.com/api/openapi/v2/budgets/...`

---

### 2. 分页机制（最容易踩坑）

`/api/openapi/v2/budgets/{id}/query` 接口的分页逻辑非常反直觉：

- **每页返回的第一个节点 (`nodes[0]`) 是当前查询的父节点自身**，不是子节点
- **真正的子节点从 `nodes[1:]` 开始**
- **`start` 参数是子节点的偏移量**，不是页码
- **`count` 字段是总节点数（含自身）**，不是子节点数

**示例:**
```
请求: GET /api/openapi/v2/budgets/$ID/query?start=0&count=100

返回:
{
  "value": {
    "count": 49222,        // 总节点数 = 自身(1) + 子节点(49221)
    "nodes": [
      { "nodeId": "root", "name": "根节点", ... },  // [0] 自身，跳过
      { "nodeId": "c1", "name": "子节点1", ... },    // [1:] 才是子节点
      { "nodeId": "c2", "name": "子节点2", ... },
      ...
    ]
  }
}
```

**分页计算:**
```go
childCount := totalCount - 1          // 子节点数 = 总数 - 自身
totalPages := (childCount + pageSize - 1) / pageSize

// 第1页: start=0 (自身 + 前99个子节点)
// 第2页: start=99 (自身 + 第100~198个子节点)
// 第3页: start=198 ...
```

---

### 3. 并发分页

第一页拿到 `count` 后，可以算出总页数，然后并发请求剩余页。每页都会返回自身节点，取 `nodes[1:]` 合并即可。

---

### 4. 预算包列表接口

`GET /api/openapi/v2/budgets?accessToken=xxx&start=0&count=100`

返回 `items` 数组，每项包含 `id` 和 `name`。注意这个接口没有分页的坑，正常分页即可。

---

### 5. Token 缓存

Token 有效期 120 分钟，代码中缓存 110 分钟后自动刷新。

---

### 6. 维度类型（dimensionType）

预算节点的 `content` 数组中每个元素都有 `dimensionType` 字段，表示维度种类：

| dimensionType | 说明 | 匹配逻辑 | API |
|--------------|------|---------|-----|
| `PROJECT` | 档案（项目、自定义档案等） | 向上找祖先（最多 5 层） | `/api/openapi/v1/dimensions/getDimensionById` |
| `DEPART` | 部门 | 向上找祖先（最多 5 层） | `/api/openapi/v1/departments/$idOrCode` |
| `FEE_TYPE` | 消费类型（费用类型） | 向上找祖先（最多 5 层） | `/api/openapi/v1/feeTypes`（全量缓存） |
| `STAFF` | 员工 | 精确匹配 | 无 |

**节点结构示例:**
```json
{
  "id": "20220419-1",
  "code": "维度-1",
  "name": "项目2",
  "content": [
    {
      "dimensionType": "PROJECT",
      "dimensionId": "项目",
      "mustLeaf": true,
      "contentId": "ID_3yrzERx0Rf0"
    }
  ],
  "children": [...]
}
```

**dimensionIds（预算包维度配置）:**
```json
"dimensionIds": {
    "项目": "PROJECT",
    "submitterId": "STAFF",
    "E_system_rank": "PROJECT",
    "expenseDepartment": "DEPART"
}
```

**匹配算法:**
- `PROJECT`：调用 `FindProjectAncestorInTree` 向上找祖先
- `DEPART`：调用 `FindDepartmentAncestorInTree` 向上找祖先
- `FEE_TYPE`：调用 `FindFeeTypeAncestorInTree` 向上找祖先（从内存缓存查找，随预算包同步刷新）
- `STAFF`：精确匹配 `contentId`

---

## 已验证的数据规模

| 预算包 | 子节点数 | 同步耗时 (10 workers) |
|--------|---------|---------------------|
| 2026成本中心预算包 | ~130 | ~3s |
| 项目预算包 | ~49221 | ~67s |
