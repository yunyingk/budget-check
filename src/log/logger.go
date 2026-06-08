package rotatelog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Period 日志轮转周期
type Period string

const (
	Daily   Period = "daily"
	Weekly  Period = "weekly"
	Monthly Period = "monthly"
)

// RotatingLogger 按日/周/月轮转的日志器
type RotatingLogger struct {
	mu         sync.Mutex
	dir        string
	period     Period
	current    string
	file       *os.File
	fallback   *os.File
	writer     io.Writer
	alsoStdout bool
	lastErrLog time.Time
}

// New 创建日志器
func New(dir string, period Period) (*RotatingLogger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	l := &RotatingLogger{
		dir:    dir,
		period: period,
	}

	if err := l.rotate(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *RotatingLogger) logFile() string {
	now := time.Now()
	switch l.period {
	case Daily:
		return filepath.Join(l.dir, now.Format("2006-01-02")+".log")
	case Weekly:
		_, w := now.ISOWeek()
		return filepath.Join(l.dir, fmt.Sprintf("%s-W%02d.log", now.Format("2006"), w))
	case Monthly:
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
	l.writer = l.buildWriter()
	log.SetOutput(l)
	return nil
}

func (l *RotatingLogger) buildWriter() io.Writer {
	if l.alsoStdout && l.file != nil {
		return io.MultiWriter(os.Stdout, l.file)
	}
	if l.alsoStdout {
		return os.Stdout
	}
	if l.file != nil {
		return l.file
	}
	return os.Stderr
}

// SetAlsoStdout 同时输出到标准输出（控制台模式用）
func (l *RotatingLogger) SetAlsoStdout() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.alsoStdout = true
	l.writer = l.buildWriter()
}

func (l *RotatingLogger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotate(); err != nil {
		if l.writer != nil {
			if time.Since(l.lastErrLog) > time.Minute {
				l.lastErrLog = time.Now()
				fmt.Fprintf(os.Stderr, "[Log] 轮转失败，继续写入当前日志文件: %v\n", err)
			}
			return l.writer.Write(p)
		}
		return os.Stderr.Write(p)
	}
	if l.writer == nil {
		return l.writeFallback(p)
	}
	n, err = l.writer.Write(p)
	if err != nil || n != len(p) {
		return l.writeFallback(p)
	}
	return n, nil
}

func (l *RotatingLogger) writeFallback(p []byte) (int, error) {
	if l.fallback == nil {
		f, err := os.OpenFile(filepath.Join(l.dir, "fallback.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			l.fallback = f
		}
	}
	if l.fallback != nil {
		return l.fallback.Write(p)
	}
	return os.Stderr.Write(p)
}

// Close 关闭日志文件
func (l *RotatingLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fallback != nil {
		l.fallback.Close()
	}
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
