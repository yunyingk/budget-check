# Budget Check Agent

> **Automated Budget Validation Agent** — Event-driven webhook agent for real-time expense budget compliance checking.

合思（易快报）预算校验智能体：基于事件驱动架构的自动化预算合规校验 Agent，通过 Webhook 接收审批事件，异步执行多维度预算规则引擎，并自动回调审批结果。

## Highlights

- **Agent Workflow** — Webhook 事件触发 → 异步任务队列 → 规则引擎校验 → 审批回调，完整闭环
- **Hierarchical In-Memory Cache** — 树形预算数据内存缓存，启动时全量同步，定时增量刷新，查询零 IO
- **Multi-branch Rule Engine** — 按费用性质（业务/管理/生产/豁免）自动路由至不同校验分支，支持多维度交叉校验
- **Fault Isolation** — Consumer 端 panic recover，单任务异常不影响队列持续消费
- **Event-Driven Architecture** — 异步解耦，Webhook 入队即返回，校验与审批流程非阻塞
- **Production-Grade Windows Service** — 原生注册为 Windows Service，开机自启，支持日志轮转

## Architecture

```
┌──────────┐    Webhook     ┌──────────────┐   Enqueue   ┌───────────┐
│  合思审批  │ ────────────► │ webhook.Handle│ ──────────► │ Task Queue│
│  Platform │  (Event Push)  └──────────────┘             └─────┬─────┘
└──────────┘                                                     │
       ▲                                                         │ Dequeue
       │ Callback                                                ▼
       │                                                 ┌──────────────┐
       │                                                 │ consumer.    │
       └───────────────────────────────────────────────── │ Process()    │
                                                         └──────┬───────┘
                                                                │
                                                         ┌──────▼───────┐
                                                         │ Rule Engine  │
                                                         │ (Multi-branch)│
                                                         └──────────────┘
```

## Rule Engine

### 费用性质路由

| 费用性质 | 校验策略 | 校验维度 |
|---------|---------|---------|
| 业务/管理 | 成本中心预算包 | 成本中心 + 明细费用档案 |
| 生产（非豁免） | 项目预算包 ∩ 成本中心预算包 | 多维交叉校验（两个都须命中） |
| 生产（豁免） | 成本中心预算包 | 同业务/管理 |
| 未知 | Reject | 直接拒绝 |

### 成本中心预算包（Hierarchical Cache）

```
成本中心预算包 (Root)
├── 成本中心 (Level 1) ← 匹配校验
│   └── 预算管控 (Level 2) ← 透传（单值节点）
│       └── 费用档案 (Level 3) ← 逐条明细校验
```

### 项目预算包（Hierarchical Cache）

```
项目预算包 (Root)
├── 项目 (Level 1) ← 匹配校验
│   └── 成本中心 (Level 2) ← 可选校验（存在则校验，不存在则 skip）
```

## API Reference

| Method | Endpoint | Description | Auth |
|--------|----------|-------------|------|
| GET | `/api/status` | Health check & cache status | — |
| GET/POST | `/api/sync` | Trigger manual cache refresh | `?password=` |
| GET | `/api/config` | Runtime configuration | `?password=` |
| POST | `/api/webhook/budget-check` | Webhook event entrypoint | — |

### Webhook Payload

```json
{
  "code": "HS2026050334",
  "flowId": "ID01T0bZEtkW1G",
  "nodeId": "FLOW:1357809991:1128586113"
}
```

### Accepted Response

```json
{
  "budget-check": "1",
  "success": true,
  "message": "已入队等待处理",
  "task_id": "260520-a1b2c3-050334"
}
```

## Deployment

### 1. 编译

```bash
GOOS=windows GOARCH=amd64 go build -o budget-check.exe ./src
```

### 2. 部署到目标机器

将 `budget-check.exe` 和 `config.yaml` 放到同一目录（如 `C:\BudgetProject\`）。

### 3. 注册为 Windows 服务

```bat
budget-check.exe -install
```

卸载服务：

```bat
budget-check.exe -uninstall
```

> 程序自动注册为 Windows 服务，开机自启，无需额外安装任何工具。

### 其他模式

```bat
# 控制台模式（调试用，直接运行即可）
budget-check.exe

# 手动同步一次后退出
budget-check.exe -sync
```

## 配置

配置文件加载优先级：
1. 命令行 `-config` 指定路径
2. `config/config.yaml`（优先）
3. `config.yaml`（兜底）

编辑 `config.yaml`，修改合思密钥、同步间隔、日志轮转等。

## 日志

- 自动创建 `logs/` 目录，日志按周期轮转
- 文件命名：daily=`2026-05-20.log`、weekly=`2026-W21.log`、monthly=`2026-05.log`
- 在 `config.yaml` 的 `logging.rotation` 中配置周期

## 接口

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| GET | /api/status | 健康检查，查看缓存状态 | 无 |
| GET/POST | /api/sync | 手动触发同步 | query `password` |
| GET | /api/config | 查看运行配置 | query `password`，密码为空时禁用 |
| POST | /api/webhook/budget-check | 单据校验入队 | 无 |

### 单据校验请求示例

```json
{
  "code": "HS2026050334",
  "flowId": "ID01T0bZEtkW1G",
  "nodeId": "FLOW:1357809991:1128586113"
}
```

### 成功响应

```json
{
  "budget-check": "1",
  "success": true,
  "message": "已入队等待处理",
  "task_id": "260520-a1b2c3-050334"
}
```

## 校验规则

### 费用性质分支

| 费用性质 | 校验逻辑 |
|---------|---------|
| 业务/管理 | 成本中心预算包：成本中心 + 明细费用档案 |
| 生产（非豁免） | 项目预算包 + 成本中心预算包（两个都命中） |
| 生产（豁免） | 同业务/管理 |
| 未知 | 直接拒绝 |

### 成本中心预算包结构

```
成本中心预算包
├── 成本中心 (level 1) ← 校验
│   └── 预算管控 (level 2) ← 跳过（只有一个固定值）
│       └── 费用档案 (level 3) ← 从明细逐条校验
```

### 项目预算包结构

```
项目预算包
├── 项目 (level 1) ← 校验
│   └── 成本中心 (level 2) ← 如果存在则校验，不存在则跳过
```

## 架构

```
HTTP请求 → webhook.Handle() → 入队 → consumer.Process() → 审批回调
```

- 服务启动即可接收请求入队
- 首次同步完成后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续消费
