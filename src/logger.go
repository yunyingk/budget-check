package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type RotatePeriod string

const (
	RotateDaily   RotatePeriod = "daily"
	RotateWeekly  RotatePeriod = "weekly"
	RotateMonthly RotatePeriod = "monthly"
)

type RotatingLogger struct {
	mu       sync.Mutex
	dir      string
	period   RotatePeriod
	current  string
	file     *os.File
	writer   io.Writer
}

func NewRotatingLogger(dir string, period RotatePeriod) (*RotatingLogger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	l := &RotatingLogger{
		dir:    dir,
		period: period,
		writer: os.Stdout,
	}

	if err := l.rotate(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *RotatingLogger) logFile() string {
	now := time.Now()
	switch l.period {
	case RotateDaily:
		return filepath.Join(l.dir, now.Format("2006-01-02")+".log")
	case RotateWeekly:
		_, w := now.ISOWeek()
		return filepath.Join(l.dir, fmt.Sprintf("%s-W%02d.log", now.Format("2006"), w))
	case RotateMonthly:
		return filepath.Join(l.dir, now.Format("2006-01")+".log")
	default:
		return filepath.Join(l.dir, now.Format("2006-01-02")+".log")
	}
}

func (l *RotatingLogger) rotate() error {
	target := l.logFile()
	if target == l.current && l.file != nil {
		return nil
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	if l.file != nil {
		l.file.Close()
	}

	l.current = target
	l.file = f
	l.writer = io.MultiWriter(os.Stdout, f)
	log.SetOutput(l.writer)
	return nil
}

func (l *RotatingLogger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotate(); err != nil {
		return os.Stderr.Write(p)
	}
	return l.writer.Write(p)
}

func (l *RotatingLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}