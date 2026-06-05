//go:build !windows

package main

import (
	"budget/src/app"
	"fmt"
)

func prepareWorkingDir(install, uninstall bool) {
}

func handleInstall() {
	fmt.Printf("合思预算校验服务 v%s\n", version)
	fmt.Println("服务注册仅支持 Windows")
}

func handleUninstall() {
	fmt.Printf("合思预算校验服务 v%s\n", version)
	fmt.Println("服务卸载仅支持 Windows")
}

func tryRunAsService(a *app.App) bool {
	return false
}

func showInteractiveMenu(a *app.App) {
	a.Init()
	a.Run()
}
