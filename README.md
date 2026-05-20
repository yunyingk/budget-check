# 合思预算校验服务

启动时自动同步预算数据到内存，提供单据校验 API。

## 部署

1. 编译 Windows 可执行文件：
   ```bash
   GOOS=windows GOARCH=amd64 go build -o budget-check.exe ./src
   ```

2. 将 `budget-check.exe` 和 `config.yaml` 放到目标机器同一目录

3. 用 nssm 注册为 Windows 服务：
   ```bat
   nssm install BudgetAPI C:\BudgetProject\budget-check.exe
   nssm set BudgetAPI AppDirectory C:\BudgetProject
   nssm start BudgetAPI
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

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/ebot/check | 单据校验（ticket_id, callback_url） |
| GET  | /api/status | 查看缓存状态 |
| POST | /api/sync | 手动触发同步 |

## 手动同步

```bash
./budget-check -sync -config config.yaml
```