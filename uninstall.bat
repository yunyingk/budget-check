@echo off
echo 正在卸载 BudgetAPI 服务...
nssm stop BudgetAPI
nssm remove BudgetAPI confirm
echo 服务已卸载
pause
