//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	status <- svc.Status{State: svc.StartPending}
	go mainLogic()
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
