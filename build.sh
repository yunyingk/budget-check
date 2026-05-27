#!/bin/bash
set -e

# 从 version.go 读取版本号（唯一真相源）
VERSION=$(grep 'version.*=' src/version.go | grep -o '"[^"]*"' | tr -d '"')
if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then
    echo "错误: 请先在 src/version.go 中设置正确的版本号"
    exit 1
fi

echo "构建版本: $VERSION"

# 确保 dist 目录存在
mkdir -p dist

# Windows 交叉编译
echo "→ Windows amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check.exe ./src

# macOS 本地编译
echo "→ macOS amd64..."
go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-mac ./src

echo "构建完成:"
ls -lh dist/budget-check.exe dist/budget-check-mac
