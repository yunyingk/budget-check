Go 项目，go.mod 在根目录，源码在 src/，运行时配置与 exe 同目录。

## 构建

```bash
GOOS=windows GOARCH=amd64 go build -o budget-check.exe ./src
```

## 关键文件

- src/main.go — 入口 + 定时同步 + 路由
- src/checker.go — 业务校验逻辑（分支规则在这里）
- src/ekb.go — 合思 API 客户端
- src/sync.go — 预算数据同步
- src/handler.go — HTTP 接口处理
- src/store.go — 内存缓存
- src/config.go — 配置加载（支持 config/ 子目录优先）
- src/logger.go — 日志轮转（daily/weekly/monthly）
- config.yaml — 运行时配置（合思密钥、端口、同步间隔、日志轮转等）