from fastapi import FastAPI, BackgroundTasks
from pydantic import BaseModel
import sqlite3
import uvicorn
import config
import requests
import json
from datetime import datetime

app = FastAPI(title="Budget Check API")

# ==========================================
# ★ 配置区域：费用性质 ID 映射表
# ==========================================
EXPENSE_NATURE_MAP = {
    "ID01LPD78hZRsr": "业务",
    "ID01LPDisfN3qv": "管理",
    "ID01LPDfjPcnyn": "生产"
}

# === 1. 请求模型 ===
class EbotRequest(BaseModel):
    ticket_id: str
    callback_url: str = ""

# === 2. 数据库工具 ===
def get_db_conn():
    return sqlite3.connect(config.DB_FILE)

# 获取 Token
def get_token():
    try:
        url = f"{config.API_HOST}/v1/auth/getAccessToken"
        payload = {"appKey": config.APP_KEY, "appSecurity": config.APP_SECURITY}
        res = requests.post(url, json=payload, timeout=5)
        if res.status_code == 200:
            return res.json().get("value", {}).get("accessToken")
        return None
    except:
        return None

# === 3. 辅助：通过 ID 查询父级 ID ===
def get_parent_id_from_ekb(dim_value_id, token):
    if not dim_value_id: return None
    url = f"{config.API_HOST}/v1/dimensions/getDimensionById"
    params = {"accessToken": token, "id": dim_value_id}
    try:
        res = requests.get(url, params=params, timeout=5)
        if res.status_code == 200:
            p_id = res.json().get("value", {}).get("parentId")
            return p_id if p_id else None
    except:
        pass
    return None

# === 4. 辅助：获取档案详情（用于识别公摊成本） ===
def get_dimension_info(dim_id, token):
    url = f"{config.API_HOST}/v1/dimensions/getDimensionById"
    params = {"accessToken": token, "id": dim_id}
    try:
        res = requests.get(url, params=params, timeout=5)
        if res.status_code == 200:
            return res.json().get("value", {})
    except:
        pass
    return None

# === 5. 查单据详情 & 提取字段 ===
def fetch_ticket_info(ticket_code, token):
    print(f">>> 正在查询单据: {ticket_code}")
    url = f"{config.API_HOST}/v1.1/flowDetails/byCode"
    params = {"accessToken": token, "code": ticket_code}
    
    try:
        res = requests.get(url, params=params, timeout=10)
        data = res.json()
        form = data.get("value", {}).get("form", {})
        if not form:
            return None, "未找到单据详情"

        # 1. 成本中心
        cost_center_id = form.get("E_system_costcenter", "")
        
        # 2. 费用类型档案 (优先从明细取)
        details = form.get("details", [])
        archive_id = ""
        if details:
            archive_id = details[0].get("feeTypeForm", {}).get("u_费用类型档案", "")
        if not archive_id:
            archive_id = form.get("u_费用类型档案", "")
        
        # 3. 项目
        project_id = form.get("项目", "")
        
        # 4. 费用性质
        nature_id = form.get("u_费用性质", "")
        expense_type_name = EXPENSE_NATURE_MAP.get(nature_id, "未知")

        print(f"    [解析] 性质: {expense_type_name}")
        print(f"    [解析] 成本中心: {cost_center_id}")
        print(f"    [解析] 费用档案: {archive_id}")
        print(f"    [解析] 项目: {project_id}")

        info = {
            "dept_code": cost_center_id,
            "archive_code": archive_id,
            "project_code": project_id,
            "expense_type": expense_type_name,
            "raw_nature_id": nature_id
        }
        return info, None
    except Exception as e:
        return None, f"解析异常: {e}"

# === 6. 核心：递归向上检查是否存在 (忽略类型) ===
def check_exists_with_traceback(dim_code, label, token):
    """
    label 参数仅用于日志打印，不再用于 SQL 过滤
    只要 ID 在库里（不管是 PROJECT 还是 DEPARTMENT），就算存在
    """
    if not dim_code: return False, None

    conn = get_db_conn()
    cursor = conn.cursor()
    
    current_id = dim_code
    iteration = 0
    max_depth = 5 
    
    try:
        while current_id and iteration < max_depth:
            # ★★★ 修改：去掉了 AND dim_type='...' 的限制 ★★★
            # 只要库里有这个 ID，不管是啥类型，都算命中
            cursor.execute("SELECT node_name, dim_type FROM budget_cache WHERE dim_code=? LIMIT 1", (current_id,))
            row = cursor.fetchone()
            
            if row:
                print(f"    [Match] {label}匹配成功: {row[0]} (Type: {row[1]})")
                return True, row[0]
            
            # 没找到，向上追溯
            print(f"    [Trace] {label} ID {current_id} 未命中，尝试查父级...")
            parent_id = get_parent_id_from_ekb(current_id, token)
            
            if not parent_id or parent_id == current_id:
                break
                
            current_id = parent_id
            iteration += 1
            
        print(f"    [Fail] {label} ({dim_code}) 最终未在预算库中找到")
        return False, None
    finally:
        conn.close()

# === 7. 业务校验逻辑 (Check Logic) ===
def check_logic(info, token):
    e_type = info['expense_type']
    d_code = info['dept_code']
    a_code = info['archive_code']
    p_code = info['project_code']
    
    print(f"[{datetime.now()}] >>> 启动校验: {e_type}")

    # 1. 校验 成本中心 (带追溯)
    ok_dept, _ = check_exists_with_traceback(d_code, "成本中心", token)
    
    # 2. 校验 费用类型档案 (带追溯)
    ok_archive, _ = check_exists_with_traceback(a_code, "费用档案", token)

    # 3. 校验 项目 (带“公摊成本”特例)
    ok_proj = False
    is_public_cost = False 
    
    if p_code:
        conn = get_db_conn()
        cursor = conn.cursor()
        # ★★★ 修改：去掉了 AND dim_type='PROJECT' ★★★
        # 只要 ID 存在就行
        cursor.execute("SELECT node_name FROM budget_cache WHERE dim_code=? LIMIT 1", (p_code,))
        row = cursor.fetchone()
        conn.close()

        if row:
            ok_proj = True
            print(f"    [Match] 项目匹配成功: {row[0]}")
        else:
            # 没找到，检查是否公摊
            print(f"    [Trace] 项目 {p_code} 未命中，检查特例...")
            p_info = get_dimension_info(p_code, token)
            p_name = p_info.get("name", "") if p_info else ""
            
            if "公摊成本" in p_name:
                is_public_cost = True
                ok_proj = True
                print(f"    [Special] 特例项目: {p_name}，豁免校验")
            else:
                print(f"    [Fail] 项目 ({p_code}) 不在预算内且非特例")
    else:
        print("    [Info] 单据无项目信息")

    result = {"pass": False, "reason": ""}
    
    # === 策略判定 ===
    basic_check = ok_dept and ok_archive
    basic_fail_reason = []
    if not ok_dept: basic_fail_reason.append("成本中心不在预算内")
    if not ok_archive: basic_fail_reason.append("费用档案不在预算内")
    
    if e_type in ["业务", "管理"]:
        if basic_check:
            result = {"pass": True, "reason": "通过"}
        else:
            result = {"pass": False, "reason": "、".join(basic_fail_reason)}
            
    elif e_type == "生产":
        if is_public_cost:
            # 公摊豁免项目校验
            if basic_check:
                result = {"pass": True, "reason": "通过(公摊豁免)"}
            else:
                result = {"pass": False, "reason": "、".join(basic_fail_reason)}
        else:
            # 正常生产逻辑
            if basic_check and ok_proj:
                result = {"pass": True, "reason": "通过"}
            else:
                reason = basic_fail_reason
                if not ok_proj: reason.append("项目不在预算内")
                result = {"pass": False, "reason": "、".join(reason)}
    
    elif e_type == "未知":
        result = {"pass": False, "reason": f"未配置性质ID: {info['raw_nature_id']}"}
    else:
        result = {"pass": False, "reason": f"暂不支持性质: {e_type}"}

    return result

# === 8. 后台任务 ===
def process_ticket_async(ticket_id, callback_url):
    token = get_token()
    if not token: return
    
    info, err = fetch_ticket_info(ticket_id, token)
    if err:
        print(f"查询失败: {err}")
        return

    res = check_logic(info, token)
    print(f">>> [Result] {res}")

    if callback_url:
        try:
            cb_data = {
                "ticket_id": ticket_id,
                "status": "PASS" if res['pass'] else "REJECT",
                "message": res['reason']
            }
            requests.post(callback_url, json=cb_data) # 上线前请解开注释
            print(f">>> [Callback] {callback_url}: {cb_data}")
        except Exception as e:
            print(f"回调失败: {e}")

@app.post("/api/ebot/check")
async def api_entry(req: EbotRequest, background_tasks: BackgroundTasks):
    background_tasks.add_task(process_ticket_async, req.ticket_id, req.callback_url)
    return {"code": 200, "message": "处理中", "ticket_id": req.ticket_id}

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000)