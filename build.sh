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

# 多平台交叉编译
echo "→ Windows amd64..."
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-windows-amd64.exe ./src
cp dist/budget-check-windows-amd64.exe dist/budget-check.exe

echo "→ Windows arm64..."
GOOS=windows GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-windows-arm64.exe ./src

echo "→ Windows 386..."
GOOS=windows GOARCH=386 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-windows-386.exe ./src

echo "→ macOS arm64..."
GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-darwin-arm64 ./src

echo "→ macOS amd64..."
GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-darwin-amd64 ./src
cp dist/budget-check-darwin-arm64 dist/budget-check-mac

echo "→ Linux amd64..."
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/budget-check-linux-amd64 ./src

# 同步配置文件到 dist
cp config.yaml dist/config.yaml
rm -rf dist/rules
cp -R rules dist/rules

echo "构建完成:"
ls -lh dist/budget-check-windows-amd64.exe dist/budget-check-windows-arm64.exe dist/budget-check-windows-386.exe dist/budget-check-darwin-arm64 dist/budget-check-darwin-amd64 dist/budget-check-linux-amd64 dist/budget-check.exe dist/budget-check-mac
echo "运行时文件:"
ls -lh dist/config.yaml dist/rules/*.json
