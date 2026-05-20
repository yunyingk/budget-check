# 项目说明

Go 项目，src/ 下是源码，config.yaml 是运行时配置。

## 构建

```bash
cd src && GOOS=windows GOARCH=amd64 go build -o budget-check.exe .
```

## 关键文件

- src/main.go — 入口 + 定时同步
- src/checker.go — 业务校验逻辑（分支规则在这里）
- src/ekb.go — 合思 API 客户端
- src/sync.go — 预算数据同步
- src/handler.go — HTTP 接口处理
- src/store.go — 内存缓存
- src/config.go — 配置加载
- config.yaml — 运行时配置（合思密钥、端口等）