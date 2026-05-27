Go 项目，go.mod 在根目录，源码在 src/，运行时配置与 exe 同目录。

## 构建

```bash
# Windows 交叉编译（产物必须输出到 dist/，版本号通过 ldflags 注入）
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=0.5.2" -o dist/budget-check.exe ./src

# macOS 本地编译（产物必须输出到 dist/，版本号通过 ldflags 注入）
go build -ldflags "-X main.version=0.5.2" -o dist/budget-check-mac ./src
```

## 版本号规则

- 每次功能更新必须同步更新 `src/version.go` 中的版本号
- bugfix 升 patch（0.5.0 → 0.5.1），功能更新升 minor（0.5.0 → 0.6.0）
- 版本号全链路显示：启动日志、/api/status、Web 页面、安装向导

## 目录结构

```
src/
├── main.go              # 入口 + 路由 + 消费循环（带 recover）
├── version.go           # 版本号定义
├── config.go            # 配置加载（支持 config/ 子目录优先）
├── handler.go           # HTTP handler（首页、状态、历史）
├── queue.go             # 通用队列（可复用）
├── logger.go            # 日志轮转（daily/weekly/monthly）
├── types/
│   └── task.go          # 公共 Task 类型
├── budget/
│   ├── store.go         # 内存缓存（树形结构 + nodeID 去重）
│   └── sync.go          # 预算数据同步 + 合思 API 客户端
├── webhook/
│   └── budget-check.go  # HTTP 入口（解析、校验、入队、返回）
└── consumer/
    └── budget-check.go  # 业务逻辑（从队列取出处理）
dist/                     # 二进制打包产物固定目录（.gitignore 忽略）
```

## 关键文件

- src/main.go — 入口 + 定时同步 + 路由 + 消费循环
- src/version.go — 版本号定义
- src/config.go — 配置加载（支持 config/ 子目录优先）
- src/handler.go — HTTP handler
- src/queue.go — 通用队列（Enqueue、QueueChan、GenTaskID）
- src/logger.go — 日志轮转（daily/weekly/monthly）
- src/types/task.go — 公共 Task 类型
- src/budget/store.go — 内存缓存（树形结构，treeCount 按 nodeID 去重）
- src/budget/sync.go — 预算数据同步 + 合思 API 客户端
- src/webhook/budget-check.go — webhook HTTP 入口
- src/consumer/budget-check.go — 业务逻辑
- config.yaml — 运行时配置（合思密钥、端口、同步间隔、日志轮转等）

## 数据流

```
HTTP请求 → webhook.Handle() → 入队 → consumer.Process() → 业务处理
```

- 服务启动即可接收请求入队
- 首次同步完成后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续消费
