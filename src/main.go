package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"budget/src/budget"
)

func main() {
	// 确保工作目录在 exe 所在目录，Windows 服务模式下工作目录默认是 System32
	if exePath, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exePath); dir != "" {
			os.Chdir(dir)
		}
	}

	configPath := flag.String("config", "config.yaml", "配置文件路径")
	syncNow := flag.Bool("sync", false, "手动同步一次后退出")
	install := flag.Bool("install", false, "注册为 Windows 服务")
	uninstall := flag.Bool("uninstall", false, "卸载 Windows 服务")
	flag.Parse()

	if *install {
		handleInstall()
		return
	}
	if *uninstall {
		handleUninstall()
		return
	}

	var err error
	cfg, err = LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	logger, err := NewRotatingLogger("logs", RotatePeriod(cfg.Logging.Rotation))
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Close()
	log.SetOutput(logger)

	if *syncNow {
		initComponents()
		budget.Sync(store, client, syncCfg)
		fmt.Printf("同步完成，缓存条目: %d\n", store.Count())
		return
	}

	// 平台特定：尝试作为 Windows 服务运行
	if tryRunAsService() {
		return
	}

	// 非服务模式（双击运行/控制台）同时输出到控制台，方便调试
	logger.SetAlsoStdout()

	// 无参数时显示交互式菜单（双击运行）
	if flag.NFlag() == 0 {
		showInteractiveMenu()
		return
	}

	// 控制台模式
	mainLogic()
}
