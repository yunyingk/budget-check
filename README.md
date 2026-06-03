# 合思预算校验服务

接收合思审批单据 Webhook，根据配置规则自动校验预算，回调审批结果。

## 快速开始

1. 把 `budget-check.exe` 和 `config.yaml` 放在同一目录
2. 把规则文件放在 `rules/` 目录下（如 `rules/budget-check.json`）
3. 双击运行，选择"启动服务"

## 架构

```
合思审批 → Webhook → 入队 → 规则引擎校验 → 审批回调
                            ↓
                     预算树匹配（多层维度）
```

- 启动时加载配置 + 编译规则引擎
- 首次同步预算数据后开始消费队列
- 消费端有 recover 保护，单条任务 panic 不影响后续

## 配置文件

### config.yaml

```yaml
# 服务端口
server:
  port: 8000

# 合思开放平台配置
ekuaibao:
  host: "https://app.ekuaibao.com"
  app_key: "你的 AppKey"
  app_secret: "你的 AppSecret"

# Webhook 配置（可配置多个）
webhooks:
  budget-check:                          # webhook 名称，对应规则文件名
    sign_key: "签名密钥"                  # 用于回调审批的签名
    targets:                             # 需要同步的预算包列表
      - id: "预算包ID"                    # 合思后台的预算包 ID
        name: "预算包名称"
    rules: "rules/budget-check.json"     # 规则文件路径（可选，默认 rules/{webhook名}.json）

# 数据同步
sync:
  interval_minutes: 60                   # 自动同步间隔（分钟）
  workers: 10                            # 同步并发数
  password: "root"                       # 手动同步密码（为空则不需要密码）
  queue_size: 100                        # 任务队列大小

# 日志
logging:
  level: "info"                          # debug / info / warn / error
  rotation: "daily"                      # daily / weekly / monthly

# Web 管理页面
web:
  enabled: true
  password: "root"
```

### rules/budget-check.json

规则文件定义每个预算包的校验工作流。文件名与 webhook 名称对应。

```json
{
  "version": 1,
  "targets": [
    {
      "id": "预算包ID",
      "name": "成本中心预算",
      "steps": [
        { "action": "split_detail" },
        { "action": "split_apportion" },
        { "action": "match_info_to_budget" }
      ]
    }
  ]
}
```

## 工作流 Steps

每个预算包（target）是一个独立工作流，`steps` 顺序执行。

### 初始数据集

开始时数据集 = `[单据表单]`，一条记录就是一个字段集合。

### 数据变换动作

| action | 说明 |
|--------|------|
| `split_detail` | 按 `details` 字段拆分为多条明细记录 |
| `split_apportion` | 按 `apportions` 字段拆分为多条分摊记录 |

### 判断动作

对数据集中每条记录执行：

| when | then | 行为 |
|------|------|------|
| `true` | `pass` | 该记录跳过后续 steps，视为通过 |
| `true` | `refuse` | 拒绝，可配置 `reason` 说明原因 |
| `true` | `commit` | 保留该记录，后续非 split 步骤跳过，直接进入最终匹配 |
| `false` | 任意 | 跳过该 step，记录继续执行后续 steps |
| 省略 | 省略 | `action` 为 `match_info_to_budget` 时执行预算包匹配 |

### Step 字段

| 字段 | 说明 |
|------|------|
| `description` | 步骤描述（配置注释，前端展示用） |
| `action` | 动作类型：`split_detail` / `split_apportion` / `match_info_to_budget` |
| `when` | 条件表达式（支持 expr-lang 语法） |
| `then` | 满足 when 时的动作：`pass` / `refuse` / `commit` |
| `reason` | 拒绝原因（then=refuse 时返回给审批系统） |

## 预算包匹配

`action: "match_info_to_budget"` 逐层匹配预算树：

1. 取当前层节点的 `dimensionId`（维度字段名）作为表单字段名
2. 用该字段的值在当前层节点中查找
3. 找不到则向上查找祖先节点（支持自定义档案的父子级关系）
4. 匹配成功进入下一层，直到叶子节点

## 规则示例

### 成本中心预算（三步全量）

```json
{
  "id": "ID01TsPQJFK1RR",
  "name": "成本中心预算",
  "steps": [
    { "description": "按费用明细拆分", "action": "split_detail" },
    { "description": "启动分摊", "action": "split_apportion" },
    { "description": "匹配成本中心预算包", "action": "match_info_to_budget" }
  ]
}
```

### 项目预算（带条件过滤）

```json
{
  "id": "ID01T5kHipEY7J",
  "name": "项目预算",
  "steps": [
    { "description": "费用性质非生产时免校验", "when": "u_费用性质 != 'ID01LPDfjPcnyn'", "then": "pass" },
    { "description": "按费用明细拆分", "action": "split_detail" },
    { "description": "指定项目免校验", "when": "项目 == 'ID01LZNNxip807'", "then": "pass" },
    { "description": "启动分摊", "action": "split_apportion" },
    { "description": "匹配项目预算包", "action": "match_info_to_budget" }
  ]
}
```

### 费用明细内部分行独立校验

每条明细独立执行后续 steps，一条明细 pass 不影响其他明细：

```
单据 → split_detail → [明细1, 明细2, 明细3]
  明细1: when 条件命中 → pass（跳过）
  明细2: 继续 → split_apportion → match_info_to_budget
  明细3: commit → 跳过后续步骤，直接进入最终匹配
```

## API 接口

| 路径 | 说明 | 认证 |
|------|------|------|
| `POST /api/webhook/{name}` | 合思 Webhook 入口 | 无 |
| `GET /api/status` | 服务状态 | 无 |
| `GET /api/rules/{webhookKey}` | 规则配置查询 | 登录 |
| `GET /api/webhooks` | Webhook 列表 | 登录 |
| `GET /api/history` | 最近处理记录 | 登录 |
| `POST /api/sync?password=xxx` | 手动触发同步 | password |
| `GET /` | 管理页面 | 登录 |

## 部署

### Windows 服务

```cmd
budget-check.exe --install    # 注册为 Windows 服务
budget-check.exe --uninstall  # 卸载服务
```

### 手动同步

```cmd
budget-check.exe --sync --config config.yaml
```

### Webhook 配置

在合思后台 → 出站消息 → 新建：
- 请求地址：`http://你的服务器:8000/api/webhook/budget-check`
- 签名密钥：与 config.yaml 中的 `sign_key` 一致

## 日志

- 自动创建 `logs/` 目录，日志按周期轮转
- 文件命名：daily=`2026-05-20.log`、weekly=`2026-W21.log`、monthly=`2026-05.log`
- 在 `config.yaml` 的 `logging.rotation` 中配置周期
