# 合思预算校验服务

启动时自动同步预算数据到内存，提供单据校验 API。

## 部署

1. 编译 Windows 可执行文件：
   ```bash
   cd src && GOOS=windows GOARCH=amd64 go build -o budget-check.exe .
   ```

2. 将 `budget-check.exe` 和 `config.yaml` 放到目标机器同一目录

3. 用 nssm 注册为 Windows 服务：
   ```bat
   nssm install BudgetAPI C:\BudgetProject\budget-check.exe
   nssm set BudgetAPI AppDirectory C:\BudgetProject
   nssm start BudgetAPI
   ```

## 配置

编辑 `config.yaml`，修改合思密钥、同步间隔等。

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