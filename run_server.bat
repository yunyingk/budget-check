@echo off
cd /d C:\BudgetProject
title Budget_API_Service
:: port 8000, 0.0.0.0 表示允许外部访问
uvicorn main:app --host 0.0.0.0 --port 8000 --workers 4
pause