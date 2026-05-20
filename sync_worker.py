import requests
import json
import sqlite3
import time
import config
from concurrent.futures import ThreadPoolExecutor, as_completed

# ==========================================
# ★★★ 严格白名单 & 深度配置 ★★★
# ==========================================
# 逻辑：只有出现在这个字典里的预算包才会被同步
# Key = 预算包名称关键词
# Value = 钻取深度 (1=只抓一层项目, 3=抓三层部门-档案)
TARGET_CONFIG = {
    "2026成本中心预算": 3,
    "项目预算包": 1
    # "2026板块预算包": 1  <-- 删掉了，绝对不会再拉取这个
}

# 并发线程数
MAX_WORKERS = 10

# === 1. 数据库工具 ===
def get_db_conn():
    return sqlite3.connect(config.DB_FILE)

def init_db():
    conn = get_db_conn()
    cursor = conn.cursor()
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS budget_cache (
            budget_id TEXT,
            budget_name TEXT,
            node_id TEXT,
            node_name TEXT,
            dim_type TEXT,
            dim_code TEXT,
            total_amount REAL,
            used_amount REAL,
            balance REAL,
            updated_at TIMESTAMP,
            PRIMARY KEY (budget_id, node_id, dim_code)
        )
    ''')
    conn.commit()
    conn.close()
    print(">>> [Init] DB Init Success")

# === 2. 抓取子节点任务 ===
def fetch_node_children(session, base_url, token, node_id):
    result_rows = []      
    next_level_ids = []   
    
    start = 0
    count = 100
    has_more = True
    
    while has_more:
        params = {"accessToken": token, "start": start, "count": count, "nodeId": node_id}
        try:
            res = session.get(base_url, params=params, timeout=20)
            if res.status_code != 200: break
            
            data = res.json()
            nodes = data.get("value", {}).get("nodes", [])
            
            if not nodes: has_more = False; break
            
            for node in nodes:
                n_id = node.get("nodeId")
                n_name = node.get("name") or node.get("code")
                is_leaf = node.get("isLeaf", True)
                
                if not is_leaf:
                    next_level_ids.append(n_id)

                contents = node.get("content", [])
                for c in contents:
                    dim_code_val = c.get("contentId")
                    did = str(c.get("dimensionId")).strip()
                    
                    dim_type = "UNKNOWN"
                    if did == "E_system_costcenter" or "costcenter" in did.lower(): 
                        dim_type = "DEPARTMENT"
                    elif did == "u_费用类型档案" or "费用类型" in did: 
                        dim_type = "ARCHIVE"
                    elif did == "项目" or "project" in did.lower() or c.get("dimensionType") == "PROJECT": 
                        dim_type = "PROJECT"

                    if dim_type != "UNKNOWN" and dim_code_val:
                        result_rows.append({
                            "node_id": n_id,
                            "node_name": n_name,
                            "dim_type": dim_type,
                            "dim_code": dim_code_val
                        })
            
            if len(nodes) < count: has_more = False
            else: start += count
        except Exception as e:
            print(f"    [Error] Fetch error: {e}")
            has_more = False
            
    return result_rows, next_level_ids

# === 3. 主同步逻辑 ===
def sync_data():
    print(f"[{time.strftime('%H:%M:%S')}] >>> Start Sync Task...")
    
    session = requests.Session()
    adapter = requests.adapters.HTTPAdapter(pool_connections=MAX_WORKERS, pool_maxsize=MAX_WORKERS, max_retries=3)
    session.mount('http://', adapter)
    session.mount('https://', adapter)

    # 获取 Token
    try:
        token_res = session.post(f"{config.API_HOST}/v1/auth/getAccessToken", 
                                 json={"appKey": config.APP_KEY, "appSecurity": config.APP_SECURITY}, timeout=10)
        token = token_res.json().get("value", {}).get("accessToken")
        print(f"    [Token] OK")
    except:
        print("!!! Token Failed"); return

    # 清空旧数据
    conn = get_db_conn()
    cursor = conn.cursor()
    print("    [Reset] Clearing old data...")
    cursor.execute("DELETE FROM budget_cache")
    conn.commit()

    # 获取列表
    try:
        budgets = session.get(f"{config.API_HOST}/v2/budgets", 
                              params={"accessToken": token, "start": 0, "count": 100}).json().get("items", [])
    except:
        print("!!! Get Budget List Failed"); return

    total_count = 0
    clean_host = config.API_HOST.strip().rstrip('/')

    for b_item in budgets:
        b_name = str(b_item.get("name")).strip()
        b_id = str(b_item.get("id")).strip()
        
        # === ★★★ 严格白名单匹配 ★★★ ===
        target_depth = 0
        is_target = False
        
        # 必须完全包含配置字典里的名字才算命中
        for key, depth in TARGET_CONFIG.items():
            if key in b_name:
                target_depth = depth
                is_target = True
                break
        
        if not is_target:
            print(f"    [Skip] 跳过: {b_name}")
            continue

        print(f"\n>>> [Target] 同步: {b_name} (Level: {target_depth})")
        
        real_id_for_url = b_id if b_id.startswith("$") else f"${b_id}"
        query_url = f"{clean_host}/v2/budgets/{real_id_for_url}/query"

        # 任务队列
        current_level_nodes = [""] 
        
        # 控制循环次数
        for level in range(target_depth):
            if not current_level_nodes:
                break
            
            print(f"    [Layer {level} -> {level+1}] Processing {len(current_level_nodes)} nodes...")

            next_level_nodes = [] 
            
            with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
                future_to_node = {executor.submit(fetch_node_children, session, query_url, token, nid): nid for nid in current_level_nodes}
                
                completed = 0
                for future in as_completed(future_to_node):
                    try:
                        rows, child_ids = future.result()
                        
                        # 补全10个字段写入
                        for r in rows:
                            cursor.execute('INSERT OR REPLACE INTO budget_cache (budget_id, budget_name, node_id, node_name, dim_type, dim_code, total_amount, used_amount, balance, updated_at) VALUES (?,?,?,?,?,?,0,0,0,CURRENT_TIMESTAMP)', 
                                           (b_id, b_name, r['node_id'], r['node_name'], r['dim_type'], r['dim_code']))
                        
                        next_level_nodes.extend(child_ids)
                        
                        completed += 1
                        if completed % 50 == 0: conn.commit()
                        
                    except Exception as e:
                        print(f"        [Warn] Node failed: {e}")

            conn.commit()
            total_count += len(current_level_nodes)
            current_level_nodes = next_level_nodes
        
        print(f"    -> {b_name} Done.")

    conn.close()
    session.close()
    print(f"\n[{time.strftime('%H:%M:%S')}] >>> ALL DONE! Nodes: {total_count}")

if __name__ == "__main__":
    init_db()
    sync_data()
