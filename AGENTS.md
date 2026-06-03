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
├── server.go            # 路由 + 消费循环（带 recover）+ initComponents
├── version.go           # 版本号定义
├── config.go            # 配置加载（config/ 子目录优先）+ LoadRules
├── handler.go           # HTTP handler（首页、状态、历史）
├── auth.go              # Web 登录 token 存储
├── queue.go             # 通用队列（Enqueue、QueueChan、GenTaskID）
├── logger.go            # 日志轮转（daily/weekly/monthly）
├── service_windows.go   # Windows 服务注册
├── service_other.go     # 非 Windows 平台占位
├── types/
│   └── task.go          # 公共 Task / Step / RuleTarget / RulesConfig
├── budget/
│   ├── store.go         # 内存缓存（树形结构 + nodeID 去重）
│   └── sync.go          # 预算数据同步
├── ekb/
│   └── client.go        # 合思 API 客户端（GetToken、FindAncestorInTree、GetDimension）
├── rules/
│   ├── engine.go        # 规则引擎（expr-lang 表达式编译 + 树匹配）
│   └── engine_test.go
├── webhook/
│   └── budget-check.go  # HTTP 入口（解析、校验、入队、返回）
├── consumer/
│   └── budget-check.go  # 业务逻辑（拉单据 → 调引擎 → 回调）
└── static/              # embed 进二进制的 Web 资源（index.html、login.html、app.js）

rules/                   # 运行时规则文件（rules/budget-check.json）
dist/                    # 二进制打包产物固定目录（.gitignore 忽略）
config.yaml              # 运行时配置（合思密钥、端口、同步间隔、日志轮转等）
```

## 关键文件

- src/main.go — 入口（参数解析、平台分发）
- src/server.go — 路由 + 消费循环 + initComponents（启动时编译规则、定时同步、HTTP 注册）
- src/version.go — 版本号定义
- src/config.go — 加载 config.yaml + LoadRules（JSON 规则文件）
- src/handler.go — HTTP handler（/、/api/status、/api/history、/api/config、/api/sync）
- src/auth.go — Web 登录 token 存储
- src/queue.go — 通用队列
- src/logger.go — 日志轮转
- src/types/task.go — Task / Step / RuleTarget / RulesConfig
- src/budget/store.go — 内存缓存（树形结构，treeCount 按 nodeID 去重）
- src/budget/sync.go — 预算数据同步
- src/ekb/client.go — 合思 API 客户端（Token 缓存、维度缓存、FindAncestorInTree 沿父级链查）
- src/rules/engine.go — 规则引擎（启动时编译 when 表达式到 vm.Program，运行期不重读）
- src/webhook/budget-check.go — webhook HTTP 入口
- src/consumer/budget-check.go — 拉单据 → 调引擎 → 回调审批
- rules/budget-check.json — 校验规则（启动时加载一次，运行期修改不生效）

## 数据流

```
HTTP请求 → webhook.Handle() → 入队 → consumer.Evaluate() → checker.CallbackApproval()
```

- 启动时 `initComponents` 加载 config.yaml + rules/budget-check.json（编译到字节码）
- 首次预算同步完成后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续消费
- 预算数据按 `cfg.Sync.IntervalMinutes` 定时重拉（storeMu Lock）；消费时 RLock 互斥
- 规则文件修改后必须重启服务才能生效（运行期不重读）
