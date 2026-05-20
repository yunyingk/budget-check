@echo off
echo 正在安装 BudgetAPI 服务...
nssm install BudgetAPI "%~dp0budget-check.exe"
nssm set BudgetAPI AppDirectory "%~dp0"
nssm set BudgetAPI AppStdout "%~dp0logs\service.log"
nssm set BudgetAPI AppStderr "%~dp0logs\service.log"
nssm start BudgetAPI
echo 服务已安装并启动
pause
