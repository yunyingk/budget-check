//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "BudgetCheck"

func handleInstall() {
	if err := installService(); err != nil {
		fmt.Printf("注册服务失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("服务注册成功！启动: sc start BudgetCheck")
}

func handleUninstall() {
	if err := uninstallService(); err != nil {
		fmt.Printf("卸载服务失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("服务已卸载")
}

func tryRunAsService() bool {
	isWinService, _ := svc.IsWindowsService()
	if !isWinService {
		return false
	}
	if err := runService(); err != nil {
		fmt.Printf("Windows 服务启动失败: %v\n", err)
		os.Exit(1)
	}
	return true
}

func runService() error {
	return svc.Run(serviceName, &budgetService{})
}

type budgetService struct{}

func (s *budgetService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Fprintf(os.Stderr, "[Service] panic: %v\n", rec)
		}
	}()

	status <- svc.Status{State: svc.StartPending}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Fprintf(os.Stderr, "[mainLogic] panic: %v\n", rec)
			}
		}()
		mainLogic()
	}()
	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}
	for {
		req := <-r
		switch req.Cmd {
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		case svc.Interrogate:
			status <- req.CurrentStatus
		}
	}
}

func installService() error {
	exePath, _ := filepath.Abs(os.Args[0])
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("服务 %s 已存在", serviceName)
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:  "合思预算校验服务",
		Description:  "预算数据同步与单据校验",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("服务 %s 不存在", serviceName)
	}
	defer s.Close()

	s.Control(svc.Stop)
	time.Sleep(2 * time.Second)
	eventlog.Remove(serviceName)
	return s.Delete()
}

func getServiceStatus() string {
	m, err := mgr.Connect()
	if err != nil {
		return "error"
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return "not_installed"
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "error"
	}

	switch status.State {
	case svc.Running:
		return "running"
	case svc.Stopped:
		return "stopped"
	default:
		return "unknown"
	}
}

func startService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Start()
}

func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	return err
}

func showInteractiveMenu() {
	status := getServiceStatus()

	fmt.Println("┌────────────────────────────────────────┐")
	fmt.Println("│     合思预算校验服务 - 安装向导        │")
	fmt.Println("├────────────────────────────────────────┤")

	var action string
	switch status {
	case "not_installed":
		fmt.Println("│  当前状态: 未安装                       │")
		fmt.Println("│                                        │")
		fmt.Println("│  → 按回车 安装并启动服务               │")
		action = "install"
	case "stopped":
		fmt.Println("│  当前状态: 已停止                       │")
		fmt.Println("│                                        │")
		fmt.Println("│  → 按回车 启动服务                     │")
		action = "start"
	case "running":
		fmt.Println("│  当前状态: 运行中                       │")
		fmt.Println("│                                        │")
		fmt.Println("│  → 按回车 停止服务                     │")
		action = "stop"
	default:
		fmt.Println("│  当前状态: 异常                         │")
		fmt.Println("│                                        │")
		fmt.Println("│  → 按回车 重新安装                     │")
		action = "install"
	}

	fmt.Println("│                                        │")
	fmt.Println("│  [Q] 退出                               │")
	fmt.Println("└────────────────────────────────────────┘")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if strings.ToUpper(input) == "Q" {
		return
	}

	switch action {
	case "install":
		fmt.Println("\n正在安装服务...")
		if err := installService(); err != nil {
			fmt.Printf("安装失败: %v\n", err)
		} else {
			fmt.Println("安装成功，正在启动...")
			if err := startService(); err != nil {
				fmt.Printf("启动失败: %v\n", err)
			} else {
				fmt.Println("服务已启动")
			}
		}
	case "start":
		fmt.Println("\n正在启动服务...")
		if err := startService(); err != nil {
			fmt.Printf("启动失败: %v\n", err)
		} else {
			fmt.Println("服务已启动")
		}
	case "stop":
		fmt.Println("\n正在停止服务...")
		if err := stopService(); err != nil {
			fmt.Printf("停止失败: %v\n", err)
		} else {
			fmt.Println("服务已停止")
		}
	}

	fmt.Println("\n按任意键退出...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
