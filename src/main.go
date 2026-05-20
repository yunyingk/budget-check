package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径（默认与exe同目录）")
	syncNow := flag.Bool("sync", false, "立即执行一次同步后退出")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("配置加载成功: 端口=%d, 合思主机=%s", cfg.Server.Port, cfg.Ekb.Host)

	store := NewStore()
	client := NewEkbClient(cfg)

	if *syncNow {
		SyncBudget(store, client, cfg.BudgetTargets)
		fmt.Printf("同步完成，缓存条目: %d\n", store.Count())
		return
	}

	go func() {
		for {
			SyncBudget(store, client, cfg.BudgetTargets)
			log.Printf("[Scheduler] 下次同步: %d 分钟后", cfg.Sync.IntervalMinutes)
			time.Sleep(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ebot/check", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		handleCheck(w, r, store, client, cfg)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		handleStatus(w, r, store)
	})
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		handleSync(w, r, store, client, cfg)
	})

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
	log.Printf("服务启动: %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}