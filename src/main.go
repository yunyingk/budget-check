package main

import (
	"flag"
	"fmt"
	"log"

	"budget/src/budget"
)

func main() {
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

	// 无参数时显示交互式菜单（双击运行）
	if flag.NFlag() == 0 {
		showInteractiveMenu()
		return
	}

	// 控制台模式
	mainLogic()
}
