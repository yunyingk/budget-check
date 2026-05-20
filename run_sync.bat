@echo off
cd /d C:\BudgetProject
echo [%date% %time%] 开始执行同步 >> sync.log
python sync_worker.py >> sync.log 2>&1
echo [%date% %time%] 执行结束 >> sync.log