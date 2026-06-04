#!/bin/bash
set -e

# 一键调试脚本：杀进程 → 构建 → 启动
# 用法: ./dev.sh

cd "$(dirname "$0")"

# 杀掉旧进程
pkill -f "budget-check-mac" 2>/dev/null || true
sleep 0.5

# 构建（只编译 macOS，跳过 Windows 交叉编译，更快）
VERSION=$(grep 'version.*=' src/version.go | grep -o '"[^"]*"' | tr -d '"')
echo "🔧 构建 v${VERSION} ..."
go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-mac ./src

# 启动（前台运行，Ctrl+C 退出）
echo "🚀 启动服务..."
./dist/budget-check-mac
