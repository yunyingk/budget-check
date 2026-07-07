Go 项目，go.mod 在根目录，源码在 src/，运行时配置与 exe 同目录。

## 构建

```bash
# 统一构建脚本（自动读取 version.go 中的版本号，通过 ldflags 注入）
./build.sh

# 手动构建（不推荐，容易输错版本号；版本号取自 src/version.go 当前值，下同）
# GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=<version>" -o dist/budget-check.exe ./src
# go build -ldflags "-X main.version=<version>" -o dist/budget-check-mac ./src
```

## 版本号规则

- 每次功能更新必须同步更新 `src/version.go` 中的版本号
- bugfix 升 patch（如 1.3.13 → 1.3.14），功能更新升 minor（如 1.3.13 → 1.4.0）
- 版本号取自 `src/version.go`（如当前 `1.3.13`），由 `build.sh` 通过 ldflags 注入，无需手动传参
- 版本号全链路显示：启动日志、/api/status、Web 页面、安装向导

## 目录结构

```
src/
├── main.go              # 入口（参数解析、平台分发）
├── version.go           # 版本号定义
├── app/
│   └── app.go           # App 容器（Config/Logger/Queue/Store/Client/Checker/Engines）
├── config/
│   ├── config.go        # Config 结构体定义
│   └── loader.go        # LoadConfig + LoadRules
├── log/
│   └── logger.go        # 日志轮转（daily/weekly/monthly），包名 rotatelog
├── queue/
│   └── queue.go         # 通用队列（非全局变量）
├── web/
│   ├── server.go        # 路由注册（动态 webhook、管理页面、API）
│   ├── webhook.go       # HTTP 入口（解析、校验、入队）
│   ├── handler.go       # 页面/状态/历史 handler
│   ├── auth.go          # Web 登录 token 存储
│   ├── auth_handler.go  # 登录/登出页面 handler
│   ├── middleware.go    # 鉴权中间件
│   └── static/          # embed 进二进制的 Web 资源
├── metrics/
│   └── metrics.go       # Prometheus 指标（队列长度、处理耗时、回调结果等）
├── types/
│   └── task.go          # Task / Step / RuleTarget / RulesConfig
├── budget/
│   ├── store.go         # 内存缓存（树形结构 + nodeID 去重）
│   └── sync.go          # 预算数据同步
├── ekb/
│   └── client.go        # 合思 API 客户端（Token 缓存、维度缓存、FindProjectAncestorInTree、FindDepartmentAncestorInTree、SyncFeeTypes、FindFeeTypeAncestorInTree）
├── rules/
│   ├── engine.go        # 规则引擎（workflow + expr-lang + 树匹配）
│   └── engine_test.go
├── consumer/
│   └── budget-check.go  # 业务逻辑（拉单据 → 按 WebhookKey 选引擎 → 回调）
├── service_windows.go   # Windows 服务注册
└── service_other.go     # 非 Windows 平台占位

rules/                   # 运行时规则文件（rules/{webhookKey}.json）
dist/                    # 二进制打包产物固定目录（.gitignore 忽略）
config.yaml              # 运行时配置（合思密钥、端口、同步间隔、日志轮转、webhooks、维度映射等）
```

## 关键文件

- `src/main.go` — 入口（参数解析、平台分发）
- `src/app/app.go` — App 容器。`Init()` 加载 config + rules，为每个 webhook 编译独立 Engine；`Run()` 启动同步循环、消费循环、HTTP Server
- `src/version.go` — 版本号定义
- `src/config/config.go` + `loader.go` — Config 结构体、加载 config.yaml + LoadRules（JSON 规则文件）
- `src/web/server.go` — 动态注册所有路由，包括遍历 `cfg.Webhooks` 注册 `/api/webhook/{name}`
- `src/web/webhook.go` — webhook HTTP 入口，将 `WebhookKey` 写入 Task
- `src/queue/queue.go` — Queue struct（非全局变量），`GenTaskID()`
- `src/log/logger.go` — `package rotatelog`，`RotatingLogger` 结构体
- `src/types/task.go` — Task（含 WebhookKey）/ Step / RuleTarget / RulesConfig
- `src/budget/store.go` — 内存缓存（树形结构，treeCount 按 nodeID 去重）
- `src/budget/sync.go` — 预算数据同步
- `src/ekb/client.go` — 合思 API 客户端（Token 缓存、维度缓存、FindProjectAncestorInTree 沿父级链查、FindDepartmentAncestorInTree 沿父级链查、SyncFeeTypes 全量同步费用类型、FindFeeTypeAncestorInTree 从缓存查找）
- `src/rules/engine.go` — 规则引擎。启动时编译 when 表达式到 vm.Program。支持 workflow（steps 顺序执行）、split_detail/split_apportion 动作、dimension_map 维度映射
- `src/consumer/budget-check.go` — 拉单据 → 按 `task.WebhookKey` 选择 Engine → 回调审批
- `rules/{webhookKey}.json` — 校验规则（启动时加载一次，运行期修改不生效）

## 规则引擎语义

### 工作流结构

每个预算包（target）是一个独立工作流，`steps` 顺序执行：

```
初始数据集 = [formUnit]
  → step1: split_detail（拆分为多条明细）
  → step2: when/then（条件判断）
  → step3: match_info_to_budget（预算包匹配）
```

### Steps（顺序执行）

初始数据集为 `[formUnit]`，每条记录是一个 `CheckUnit`（`Label` + `Fields map`）。

**数据变换动作**（改变数据集）：
- `action=split_detail` → 从 `details` 字段拆分为多条记录，合并 `feeTypeForm` 字段
- `action=split_apportion` → 从 `apportions` 字段拆分为多条记录，合并 `apportionForm` 字段

**判断动作**（对每条记录执行）：
- `when=true && then=pass` → 该记录跳过后续 steps（通过）
- `when=true && then=refuse` → 拒绝，可配置 `reason` 说明原因
- `when=false` → 跳过该 step，记录继续执行后续 steps
- `action=match_info_to_budget` → 执行预算包匹配

### Dimension Map（维度映射）

预算树匹配时需要把 `DimType` 映射到表单字段名。配置在 `config.yaml` 全局中：

```yaml
dimension_map:
  costCenter: "E_system_costcenter"
  project: "项目"
  feeType: "u_费用类型档案"
```

默认已内置 `costCenter`→`E_system_costcenter`、`project`→`项目`、`feeType`→`u_费用类型档案`。企业可在 `config.yaml` 中覆盖。

### CheckUnit（校验单元）

```go
type CheckUnit struct {
    Label  string
    Fields map[string]interface{}
}
```

所有字段动态存取，无任何硬编码业务字段。`split_detail` 后 Fields 包含 form + detail + feeTypeForm 的合并字段。

## 数据流

```
HTTP请求 → web.HandleWebhook() → 入队 → consumer.Evaluate() → checker.CallbackApproval()
                              ↑
                    按 WebhookKey 选 Engine + SignKey
```

- 启动时 `app.Init()` 加载 config.yaml + rules/{key}.json（为每个 webhook 编译独立 Engine）
- 首次预算同步完成后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续消费
- 预算数据按 `cfg.Sync.IntervalMinutes` 定时重拉（storeMu Lock）；消费时 RLock 互斥
- 规则文件修改后必须重启服务才能生效（运行期不重读）
