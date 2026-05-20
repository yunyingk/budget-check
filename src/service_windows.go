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

// runService 启动 Windows 服务模式
func runService() error {
	return svc.Run(serviceName, &budgetService{})
}

// budgetService 实现 svc.Handler
type budgetService struct{}

func (s *budgetService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	// 启动主逻辑（异步）
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

// installService 注册 Windows 服务
func installService() error {
	exePath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("服务 %s 已存在，请先卸载", serviceName)
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:  "合思预算校验服务",
		Description:  "启动时自动同步预算数据到内存，提供单据校验和审批回调",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	// 注册事件日志
	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return err
	}

	return nil
}

// uninstallService 卸载 Windows 服务
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

	// 先停止服务
	s.Control(svc.Stop)
	time.Sleep(2 * time.Second)

	// 删除事件日志
	eventlog.Remove(serviceName)

	return s.Delete()
}
