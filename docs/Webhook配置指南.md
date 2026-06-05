# Webhook 配置指南

## 概述

本服务支持配置多个 Webhook，每个 Webhook 对应一个独立的校验场景（如成本中心预算、项目预算等）。

每个 Webhook 有：
- **独立的路由**：`/api/webhook/{name}`
- **独立的签名密钥**：用于回调审批时的签名验证
- **独立的预算包列表**：需要同步的预算包 ID
- **独立的校验规则**：定义在 `rules/{name}.json`

---

## 配置结构

在 `config.yaml` 中配置 Webhook：

```yaml
webhooks:
  budget-check:                          # Webhook 名称，对应路由 /api/webhook/budget-check
    sign_key: "l5fY6tdAf9W7"            # 出站消息签名密钥
    targets:                             # 需要同步的预算包列表
      - id: "ID01TsPQJFK1RR"            # 预算包 ID（从合思获取）
        name: "成本中心预算"              # 显示名称（仅用于日志和前端展示）
      - id: "ID01T5kHipEY7J"
        name: "项目预算"
    rules: "rules/budget-check.json"     # 校验规则文件路径

  # 可以配置多个 Webhook
  another-check:
    sign_key: "another_key"
    targets:
      - id: "ID01XXXXXX"
        name: "另一个预算包"
    rules: "rules/another-check.json"
```

---

## 字段说明

| 字段 | 必填 | 说明 |
|------|------|------|
| `sign_key` | 是 | 出站消息签名密钥，用于回调审批时的签名验证。需要与合思审批流配置中的密钥一致 |
| `targets` | 是 | 需要同步的预算包列表。每个 target 包含 `id` 和 `name` |
| `targets[].id` | 是 | 预算包 ID，从合思开放平台获取 |
| `targets[].name` | 否 | 预算包显示名称，仅用于日志和前端展示 |
| `rules` | 是 | 校验规则文件路径，相对于程序运行目录 |

---

## 预算包 ID 获取方法

1. 登录合思开放平台
2. 进入 **预算管理** → **预算包列表**
3. 找到目标预算包，复制其 ID

或者调用合思 API 获取：

```
GET https://app.ekuaibao.com/api/openapi/v2/budgets?accessToken=xxx&start=0&count=100
```

返回示例：
```json
{
  "items": [
    { "id": "ID01TsPQJFK1RR", "name": "2026成本中心预算包" },
    { "id": "ID01T5kHipEY7J", "name": "项目预算包" }
  ]
}
```

---

## 合思审批流配置

在合思审批流中配置 Webhook 回调：

1. 进入 **审批流设计**
2. 添加 **Webhook 节点**
3. 配置：
   - **请求地址**：`http://你的服务器IP:8000/api/webhook/budget-check`
   - **请求方式**：`POST`
   - **请求体**：
     ```json
     {
       "code": "${flowCode}",
       "flowId": "${flowId}",
       "nodeId": "${nodeId}"
     }
     ```

---

## 多 Webhook 场景

如果企业有多个独立的校验场景，可以配置多个 Webhook：

```yaml
webhooks:
  # 成本中心预算校验
  cost-center-check:
    sign_key: "key1"
    targets:
      - id: "ID01TsPQJFK1RR"
        name: "成本中心预算"
    rules: "rules/cost-center-check.json"

  # 项目预算校验
  project-check:
    sign_key: "key2"
    targets:
      - id: "ID01T5kHipEY7J"
        name: "项目预算"
    rules: "rules/project-check.json"

  # 板块预算校验
  sector-check:
    sign_key: "key3"
    targets:
      - id: "ID01PIbqQYosvt"
        name: "板块预算"
    rules: "rules/sector-check.json"
```

每个 Webhook 有独立的：
- 路由：`/api/webhook/cost-center-check`、`/api/webhook/project-check`、`/api/webhook/sector-check`
- 签名密钥
- 预算包同步列表
- 校验规则

---

## 示例：完整配置

```yaml
server:
  port: 8000

ekuaibao:
  host: "https://app.ekuaibao.com"
  app_key: "你的AppKey"
  app_secret: "你的AppSecret"

webhooks:
  budget-check:
    sign_key: "你的签名密钥"
    targets:
      - id: "ID01TsPQJFK1RR"
        name: "成本中心预算"
      - id: "ID01T5kHipEY7J"
        name: "项目预算"
      - id: "ID01PIbqQYosvt"
        name: "板块预算"
    rules: "rules/budget-check.json"

sync:
  interval_minutes: 60
  workers: 10
  queue_size: 100

logging:
  level: "info"
  rotation: "daily"

web:
  enabled: true
  password: "root"
  admin_password: "admin123"
```

---

## 常见问题

**Q: 如何添加新的预算包？**

1. 在 `config.yaml` 的 `targets` 中添加新的预算包 ID
2. 在 `rules/{name}.json` 中添加对应的校验规则
3. 重启服务

**Q: 多个 Webhook 可以用同一个签名密钥吗？**

可以，但建议每个 Webhook 使用独立的密钥，便于管理和安全审计。

**Q: 预算包 ID 填错了会怎样？**

服务启动时会尝试同步预算包，如果 ID 不存在，会在日志中报错，但不会阻止服务启动。校验时会提示"预算包未同步"。

**Q: rules 文件路径怎么填？**

相对于程序运行目录。例如：
- 程序在 `C:\BudgetProject\`
- 规则文件在 `C:\BudgetProject\rules\budget-check.json`
- 配置填 `rules/budget-check.json`
