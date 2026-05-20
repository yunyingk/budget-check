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
  "cache_count": 49352,
  "updated_at": "2026-05-20T20:00:00+08:00"
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

### 4. 单据校验 `POST /api/ebot/check`（待实现）

接收合思机器人回调，校验单据预算。

**请求体:**
```json
{
  "ticket_id": "单据ID",
  "callback_url": "回调地址（可选）"
}
```

**响应示例:**
```json
{
  "code": 200,
  "message": "处理中",
  "ticket_id": "xxx"
}
```

---

## 认证规则总结

| 接口 | 认证方式 | 密码为空时 |
|------|----------|-----------|
| `/api/status` | 无 | 正常工作 |
| `/api/sync` | query `password` | 无需密码即可调用 |
| `/api/config` | query `password` | 接口禁用（返回 404） |
| `/api/ebot/check` | 无 | 正常工作 |
