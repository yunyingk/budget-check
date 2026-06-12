package main

import (
	"budget/src/app"
	"budget/src/config"
	rotatelog "budget/src/log"
	"flag"
	"fmt"
	"log"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	syncNow := flag.Bool("sync", false, "手动同步一次后退出")
	install := flag.Bool("install", false, "注册为 Windows 服务")
	uninstall := flag.Bool("uninstall", false, "卸载 Windows 服务")
	flag.Parse()

	prepareWorkingDir(*install, *uninstall)

	if *install {
		handleInstall()
		return
	}
	if *uninstall {
		handleUninstall()
		return
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	logger, err := rotatelog.New("logs", rotatelog.Period(cfg.Logging.Rotation))
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
	defer logger.Close()
	log.SetOutput(logger)

	a := app.New(cfg, logger)
	a.Version = version

	if *syncNow {
		fmt.Printf("合思预算校验服务 v%s\n", version)
		a.Init()
		if err := a.Sync(); err != nil {
			log.Fatalf("同步失败: %v", err)
		}
		fmt.Printf("同步完成，缓存条目: %d\n", a.Store.Count())
		return
	}

	// 平台特定：尝试作为 Windows 服务运行
	if tryRunAsService(a) {
		return
	}

	// 非服务模式（双击运行/控制台）同时输出到控制台，方便调试
	logger.SetAlsoStdout()

	// 无参数时显示交互式菜单（双击运行）
	if flag.NFlag() == 0 {
		showInteractiveMenu(a)
		return
	}

	// 控制台模式
	a.Init()
	if err := a.Run(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
