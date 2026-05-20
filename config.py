# config.py

# === 基础环境配置 ===
# 在本地 Mac 上测试时设为 'LOCAL'，上线后设为 'PROD'
ENV = 'LOCAL'

# === 合思 API 配置 ===
APP_KEY = "9c5b3631-0e6a-4b3e-afcd-7d6fcfc377ca"
APP_SECURITY = "d866a007-78a3-4bfd-972e-9ea1bf87a3d5"
API_HOST = "https://app.ekuaibao.com/api/openapi"

# === 数据库配置 ===
# 本地 SQLite 文件名
DB_FILE = "budget_data.db"
# 生产环境 SQL Server 连接串 (先放着备用)
SQL_SERVER_CONN = "DRIVER={SQL Server};SERVER=127.0.0.1;DATABASE=master;UID=sa;PWD=Njch123"