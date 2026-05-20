# 合思预算校验服务

启动时自动同步预算数据到内存，提供单据校验 API。

## 部署

### 1. 编译

```bash
GOOS=windows GOARCH=amd64 go build -o budget-check.exe ./src
```

### 2. 部署到目标机器

将以下文件放到同一目录（如 `C:\BudgetProject\`）：
- `budget-check.exe`
- `config.yaml`
- `install.bat`（见下方）

### 3. 一键安装为 Windows 服务

双击 `install.bat` 即可注册并启动服务：

```bat
@echo off
nssm install BudgetAPI "%~dp0budget-check.exe"
nssm set BudgetAPI AppDirectory "%~dp0"
nssm set BudgetAPI AppStdout "%~dp0logs\service.log"
nssm set BudgetAPI AppStderr "%~dp0logs\service.log"
nssm start BudgetAPI
echo 服务已安装并启动
pause
```

卸载服务：

```bat
@echo off
nssm stop BudgetAPI
nssm remove BudgetAPI confirm
echo 服务已卸载
pause
```

> 需要先安装 [nssm](https://nssm.cc/)，或者用 `sc` 命令（Windows 自带，但功能有限）。

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

## 手动同步

```bash
./budget-check.exe -sync -config config.yaml
```

## 架构

```
HTTP请求 → webhook.Handle() → 入队 → consumer.Process() → 审批回调
```

- 服务启动即可接收请求入队
- 首次同步完成后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续消费
