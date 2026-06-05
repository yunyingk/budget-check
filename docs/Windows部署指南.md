# Windows 部署指南

## 功能说明

本程序是一个**预算校验服务**，主要功能：
- 从合思系统同步预算数据到内存
- 接收单据校验请求，判断是否超出预算
- 提供 Web 管理页面查看状态

程序会注册为 **Windows 服务**，具备：
- **开机自启**：服务器重启后自动运行
- **后台运行**：无窗口，不占用桌面
- **崩溃恢复**：服务异常退出后 Windows 自动重启

## 快速安装

### 1. 准备文件

将以下文件放到服务器目录（如 `C:\BudgetProject\`）：

```
C:\BudgetProject\
├── budget-check.exe    # 主程序
├── config.yaml         # 配置文件
└── rules\              # 规则文件目录
    └── budget-check.json
```

### 2. 编辑配置

用记事本打开 `config.yaml`，修改以下内容：

```yaml
# 合思开放平台凭证（从合思管理员后台获取）
ekuaibao:
  app_key: "你的 AppKey"
  app_secret: "你的 AppSecret"

# HTTP 服务端口（默认 8000，按需修改）
server:
  port: 8000

# Webhook 签名密钥在 webhooks 下配置
webhooks:
  budget-check:
    sign_key: "你的 SignKey"
    rules: "rules/budget-check.json"
    targets:
      - id: "预算包ID"
        name: "预算包名称"
```

### 3. 安装服务

**双击 `budget-check.exe`**，会看到：

```
┌────────────────────────────────────────┐
│     合思预算校验服务 - 安装向导        │
├────────────────────────────────────────┤
│  当前状态: 未安装                       │
│                                        │
│  → 按回车 安装并启动服务               │
│                                        │
│  [Q] 退出                               │
└────────────────────────────────────────┘
```

**按回车** → 自动安装并启动服务。

### 4. 验证安装

打开浏览器访问：`http://服务器IP:8000/api/status`

看到类似响应说明安装成功：

```json
{
  "status": "ok",
  "cached_count": 1234,
  "last_sync": "2026-05-21T10:00:00Z"
}
```

## 服务管理

### 启动 / 停止服务

**方法一：双击 exe**

双击 `budget-check.exe`，程序会自动检测状态：
- 未安装 → 提示安装
- 已停止 → 提示启动
- 运行中 → 提示停止

**方法二：Windows 服务管理器**

1. 按 `Win + R`，输入 `services.msc`，回车
2. 找到 **合思预算校验服务**
3. 右键 → 启动 / 停止 / 重启

**方法三：命令行（管理员权限）**

```bat
# 启动
sc start BudgetCheck

# 停止
sc stop BudgetCheck

# 查看状态
sc query BudgetCheck
```

### 卸载服务

双击 `budget-check.exe`，输入 `Q` 退出后，用命令行：

```bat
budget-check.exe -uninstall
```

## 日志

日志自动保存在程序目录下的 `logs/` 文件夹：

```
logs/
├── 2026-05-21.log    # daily 轮转
├── 2026-05-22.log
└── ...
```

可在 `config.yaml` 中修改轮转周期：

```yaml
logging:
  rotation: "daily"    # daily / weekly / monthly
```

## 常见问题

**Q: 端口被占用怎么办？**

修改 `config.yaml` 中的 `server.port`，然后重启服务。

**Q: 如何查看服务是否正常运行？**

访问 `http://服务器IP:8000/api/status`，或查看 `logs/` 目录下的最新日志。

**Q: 服务启动失败？**

1. 检查 `config.yaml` 格式是否正确
2. 检查端口是否被占用
3. 查看 Windows 事件查看器（eventvwr.msc）中的错误信息

---

## 内部逻辑（开发参考）

### 服务生命周期

```
双击 exe
    ↓
检测是否为 Windows 服务模式
    ├─ 是 → 运行服务（监听 Stop/Shutdown 信号）
    └─ 否 → 显示交互式菜单
                ↓
            按回车
                ↓
            安装服务（StartAutomatic）
                ↓
            启动服务
                ↓
            按任意键关闭窗口
```

### 数据同步流程

```
启动
  ├─ 启动 HTTP 服务（接收校验请求）
  ↓
后台首次全量同步（从合思 API 拉取预算包）
  ↓
首次同步完成后开始消费队列
  ↓
定时同步（每 N 分钟重新拉取）
```

### 校验逻辑分支

```
收到单据
  ↓
提取费用性质
  ├─ 业务/管理 → 校验成本中心 + 费用档案
  ├─ 生产（非豁免）→ 校验项目 + 成本中心
  ├─ 生产（豁免）→ 同业务/管理
  └─ 未知 → 直接拒绝
  ↓
返回结果（通过/拒绝 + 原因）
```

### 关键配置项

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `server.port` | HTTP 端口 | 8000 |
| `sync.interval_minutes` | 同步间隔 | 60 |
| `sync.workers` | 同步并发数 | 10 |
| `logging.rotation` | 日志轮转 | daily |
| `web.enabled` | 启用管理页面 | true |
