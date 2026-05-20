# ebot_receiver.py
from fastapi import FastAPI, Request
import uvicorn

app = FastAPI()

@app.post("/ebot/callback")
async def receive_callback(request: Request):
    data = await request.json()
    print("\n" + "="*30)
    print("📢 收到 ebot 回调结果！")
    print(f"单据编号: {data.get('ticket_id')}")
    print(f"审批状态: {data.get('status')}")
    print(f"原因备注: {data.get('message')}")
    print("="*30 + "\n")
    return {"status": "success"}

if __name__ == "__main__":
    # 启动在 8001 端口，不要和 main.py 的 8000 冲突
    uvicorn.run(app, host="0.0.0.0", port=8001)