Go 项目，go.mod 在根目录，源码在 src/，运行时配置与 exe 同目录。

## 构建

```bash
# 统一构建脚本（自动读取 version.go 中的版本号，通过 ldflags 注入）
./build.sh

# 手动构建（不推荐，容易输错版本号）
# GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=0.5.2" -o dist/budget-check.exe ./src
# go build -ldflags "-X main.version=0.5.2" -o dist/budget-check-mac ./src
```

## 版本号规则

- 每次功能更新必须同步更新 `src/version.go` 中的版本号
- bugfix 升 patch（0.5.0 → 0.5.1），功能更新升 minor（0.5.0 → 0.6.0）
- 版本号全链路显示：启动日志、/api/status、Web 页面、安装向导

## 目录结构

```
src/
├── main.go              # 入口（参数解析、Windows 服务、菜单）
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
│   └── static/          # embed 进二进制的 Web 资源
├── types/
│   └── task.go          # Task / Step / RuleTarget / RulesConfig
├── budget/
│   ├── store.go         # 内存缓存（树形结构 + nodeID 去重）
│   └── sync.go          # 预算数据同步
├── ekb/
│   └── client.go        # 合思 API 客户端（Token 缓存、维度缓存、FindAncestorInTree）
├── rules/
│   ├── engine.go        # 规则引擎（global_steps + split_mode + expr-lang + 树匹配）
│   └── engine_test.go
├── consumer/
│   └── budget-check.go  # 业务逻辑（拉单据 → 按 WebhookKey 选引擎 → 回调）
├── service_windows.go   # Windows 服务注册
└── service_other.go     # 非 Windows 平台占位

rules/                   # 运行时规则文件（rules/{webhookKey}.json）
dist/                    # 二进制打包产物固定目录（.gitignore 忽略）
config.yaml              # 运行时配置（合思密钥、端口、同步间隔、日志轮转、webhooks 等）
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
- `src/types/task.go` — Task（含 WebhookKey）/ Step / RuleTarget / RulesConfig（含 GlobalSteps + SplitMode）
- `src/budget/store.go` — 内存缓存（树形结构，treeCount 按 nodeID 去重）
- `src/budget/sync.go` — 预算数据同步
- `src/ekb/client.go` — 合思 API 客户端（Token 缓存、维度缓存、FindAncestorInTree 沿父级链查）
- `src/rules/engine.go` — 规则引擎。启动时编译 when 表达式到 vm.Program。支持 global_steps（单据级前置过滤）、split_mode（显式拆分）、detail 字段覆盖 form
- `src/consumer/budget-check.go` — 拉单据 → 按 `task.WebhookKey` 选择 Engine → 回调审批
- `rules/{webhookKey}.json` — 校验规则（启动时加载一次，运行期修改不生效）

## 规则引擎语义

### Global Steps（单据级别前置规则）

在 `Evaluate` 开始时执行，基于 form（单据表头）字段：
- `when=true && then=pass` → 整个单据 **accept**
- `when=true && then=refuse` → 整个单据 **refuse**
- `when=false` → 跳过该 step，继续下一个

### Target Steps（预算包级别规则）

对每个校验单元（unit）执行：
- `when=true && then=pass` → 该 target 跳过（不拒绝）
- `when=true && then=refuse` → 该 target 拒绝
- `when=false` → 跳过该 step
- `action=match_info_to_budget` → 执行预算包匹配

### Split Mode（拆分模式）

- `""`（默认）— 不拆分，form 作为一条记录
- `"detail"` — 按 details 拆分为多条记录（detail 字段覆盖 form 同名字段）
- `"apportion"` — 按 details 拆分，有 apportions 的明细进一步按分摊拆分

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
