//go:build !windows

package main

import "fmt"

func handleInstall() {
	fmt.Printf("合思预算校验服务 v%s\n", version)
	fmt.Println("服务注册仅支持 Windows")
}

func handleUninstall() {
	fmt.Printf("合思预算校验服务 v%s\n", version)
	fmt.Println("服务卸载仅支持 Windows")
}

func tryRunAsService() bool {
	return false
}

func showInteractiveMenu() {
	fmt.Println("交互式菜单仅支持 Windows")
}
