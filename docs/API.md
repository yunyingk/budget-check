# 预算校验服务 API 文档

## 基础信息

- 默认端口: 8000
- 响应格式: JSON
- 时间格式: RFC3339 (带时区偏移，如 `2026-05-20T21:04:05+08:00`)

---

## 接口列表

### 1. 健康检查 `GET /api/status`

无需认证，用于探活。

**响应示例:**
```json
{
  "status": "ok",
  "cached_count": 49352,
  "last_sync": "2026-05-20T20:00:00+08:00",
  "version": "1.0.0",
  "sync_interval": 60,
  "queue_size": 100
}
```

---

### 2. 手动同步 `GET/POST /api/sync`

触发一次全量预算数据同步。同步在后台异步执行，接口立即返回。

**认证:** query 参数 `password`（配置文件中 `sync.password` 为空时无需密码）

**请求示例:**
```
GET http://host:8000/api/sync?password=root
POST http://host:8000/api/sync?password=root
```

**响应示例:**
```json
{
  "success": true,
  "message": "同步已启动",
  "started_at": "2026-05-20T21:04:05+08:00",
  "last_sync_at": "2026-05-20T20:00:00+08:00",
  "client_ip": "192.168.1.100:52341",
  "current_count": 49352,
  "workers": 10
}
```

**错误响应 (密码错误):**
```json
{
  "error": "密码错误"
}
```

---

### 3. 查看配置 `GET /api/config`

返回当前运行的完整配置内容。**必须设置密码才生效**，密码为空时返回 404。

**认证:** query 参数 `password`（与手动同步共用同一密码）

**请求示例:**
```
GET http://host:8000/api/config?password=root
```

**响应示例:** 返回 config.yaml 的完整 JSON 结构

---

### 4. 单据校验 `POST /api/webhook/{name}`

接收合思回调，将校验任务入队异步处理。服务启动即可接收，首次同步完成后开始消费。

**路径参数:**
- `name`: Webhook 名称，对应 config.yaml 中的 webhooks 配置

**请求体:**
```json
{
  "code": "HS2026050334",
  "flowId": "ID01T0bZEtkW1G",
  "nodeId": "FLOW:1357809991:1128586113"
}
```

**响应示例 (成功入队):**
```json
{
  "budget-check": "1",
  "success": true,
  "message": "已入队等待处理",
  "task_id": "260520-a1b2c3-050334",
  "pending": 3
}
```

**字段说明:**
- `budget-check`: "1" 表示成功，"0" 表示失败（合思关键字匹配用）
- `task_id`: 任务唯一ID，格式 `YYMMDD-随机6位-单号后6位`
- `pending`: 入队时前面还有几条待处理

**错误响应:**
```json
{
  "budget-check": "0",
  "success": false,
  "message": "code、flowId、nodeId 不能为空"
}
```

---

### 5. 获取规则配置 `GET /api/rules/{webhookKey}`

获取指定 Webhook 的规则配置。

**认证:** 需要 Web 登录

**路径参数:**
- `webhookKey`: Webhook 名称

**请求示例:**
```
GET http://host:8000/api/rules/budget-check
```

**响应示例:**
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
    }
  ]
}
```

**错误响应:**
```json
{
  "error": "规则配置不存在: unknown-key"
}
```

---

### 6. 获取 Webhook 列表 `GET /api/webhooks`

获取所有配置的 Webhook 列表。

**认证:** 需要 Web 登录

**请求示例:**
```
GET http://host:8000/api/webhooks
```

**响应示例:**
```json
{
  "webhooks": [
    {
      "name": "budget-check",
      "sign_key": "l5fY****9W7",
      "targets": [
        { "id": "ID01TsPQJFK1RR", "name": "成本中心预算" },
        { "id": "ID01T5kHipEY7J", "name": "项目预算" },
        { "id": "ID01PIbqQYosvt", "name": "板块预算" }
      ],
      "rules_file": "rules/budget-check.json"
    }
  ]
}
```

**注意:** `sign_key` 会脱敏显示，只显示前4位和后3位。

---

### 7. 获取历史记录 `GET /api/history`

获取校验任务的历史记录。

**认证:** 需要 Web 登录

**请求示例:**
```
GET http://host:8000/api/history
```

**响应示例:**
```json
{
  "history": [
    {
      "task_id": "260520-a1b2c3-050334",
      "code": "HS2026050334",
      "status": "accept",
      "message": "同意",
      "created_at": "2026-05-20T21:04:05+08:00",
      "completed_at": "2026-05-20T21:04:06+08:00"
    }
  ]
}
```

---

### 8. 登录页面 `GET /login`

返回登录页面 HTML。

---

### 9. 登录 `POST /api/login`

用户登录，获取 token。

**请求体:**
```json
{
  "password": "root"
}
```

**响应示例:**
```json
{
  "success": true,
  "token": "abc123..."
}
```

---

### 10. 登出 `POST /api/logout`

用户登出，清除 token。

**认证:** 需要 Web 登录

**响应示例:**
```json
{
  "success": true
}
```

---

## 认证规则总结

| 接口 | 认证方式 | 密码为空时 |
|------|----------|-----------|
| `/api/status` | 无 | 正常工作 |
| `/api/sync` | query `password` | 无需密码即可调用 |
| `/api/config` | query `password` | 接口禁用（返回 404） |
| `/api/webhook/{name}` | 无 | 正常工作 |
| `/api/rules/{key}` | Web 登录 | 需要登录 |
| `/api/webhooks` | Web 登录 | 需要登录 |
| `/api/history` | Web 登录 | 需要登录 |
| `/login` | 无 | 正常工作 |
| `/api/login` | 无 | 正常工作 |
| `/api/logout` | Web 登录 | 需要登录 |

---

## Web 管理页面

启用 Web 管理页面后，访问 `http://host:8000/` 可查看：

1. **概览**: 服务状态、缓存数量、最后同步时间
2. **规则工作流**: 各 Webhook 的规则配置可视化
3. **Webhooks**: Webhook 列表、签名密钥、关联的预算包

需要登录才能访问（密码在 config.yaml 中配置）。
