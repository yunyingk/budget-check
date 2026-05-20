package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleStatus(w http.ResponseWriter, r *http.Request, store interface {
	Count() int
	UpdatedAt() time.Time
}) {
	lastSync := store.UpdatedAt()
	lastSyncStr := ""
	if !lastSync.IsZero() {
		lastSyncStr = lastSync.Format(time.RFC3339)
	}
	writeJSON(w, 200, map[string]interface{}{
		"status":        "ok",
		"count":         store.Count(),
		"last_sync_at":  lastSyncStr,
	})
}

func handleHome(w http.ResponseWriter, r *http.Request, store interface {
	Count() int
	UpdatedAt() time.Time
}, cfg *Config) {
	if !cfg.Web.Enabled {
		http.Error(w, "Web 管理页面未启用", http.StatusNotFound)
		return
	}

	if cfg.Web.Password != "" && r.URL.Query().Get("password") != cfg.Web.Password {
		http.Error(w, "密码错误", http.StatusForbidden)
		return
	}

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>合思预算校验服务</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; color: #333; }
  .container { max-width: 800px; margin: 0 auto; padding: 20px; }
  .card { background: #fff; border-radius: 8px; padding: 20px; margin-bottom: 16px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
  .card h2 { font-size: 16px; color: #666; margin-bottom: 12px; }
  .status-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #f0f0f0; }
  .status-row:last-child { border-bottom: none; }
  .label { color: #999; }
  .value { font-weight: 500; }
  .btn { display: inline-block; padding: 10px 20px; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; text-decoration: none; }
  .btn-primary { background: #1677ff; color: #fff; }
  .btn-primary:hover { background: #4096ff; }
  .actions { display: flex; gap: 12px; margin-top: 16px; }
  .tip { font-size: 12px; color: #999; margin-top: 8px; }
  .tag { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
  .tag-ok { background: #f6ffed; color: #52c41a; border: 1px solid #b7eb8f; }
</style>
</head>
<body>
<div class="container">
  <div class="card">
    <h2>服务状态</h2>
    <div class="status-row">
      <span class="label">缓存条目</span>
      <span class="value">%d</span>
    </div>
    <div class="status-row">
      <span class="label">上次同步</span>
      <span class="value">%s</span>
    </div>
    <div class="status-row">
      <span class="label">同步间隔</span>
      <span class="value">%d 分钟</span>
    </div>
    <div class="status-row">
      <span class="label">队列容量</span>
      <span class="value">%d</span>
    </div>
  </div>

  <div class="card">
    <h2>预算目标</h2>
    %s
  </div>

  <div class="card">
    <h2>费用性质映射</h2>
    %s
  </div>

  <div class="card">
    <h2>操作</h2>
    <div class="actions">
      <button class="btn btn-primary" onclick="doSync()">手动同步</button>
      <a class="btn btn-primary" href="/api/status" target="_blank">状态 API</a>
    </div>
    <div class="tip">手动同步需要密码验证（如已配置）</div>
  </div>
</div>
<script>
function doSync() {
  var pwd = prompt('输入同步密码（如已配置）');
  fetch('/api/sync' + (pwd ? '?password=' + encodeURIComponent(pwd) : ''), {method:'POST'})
    .then(function(r){return r.json()})
    .then(function(d){alert(d.message || JSON.stringify(d))})
    .catch(function(e){alert('请求失败: ' + e)});
}
</script>
</body>
</html>`

	lastSync := store.UpdatedAt()
	lastSyncStr := "未同步"
	if !lastSync.IsZero() {
		lastSyncStr = lastSync.Format("2006-01-02 15:04:05")
	}

	targetsHTML := ""
	for _, t := range cfg.BudgetTargets {
		targetsHTML += fmt.Sprintf(`<div class="status-row">
			<span class="label">%s</span>
			<span class="value"><span class="tag tag-ok">深度 %d</span></span>
		</div>`, t.Name, t.Depth)
	}

	natureHTML := ""
	for id, name := range cfg.ExpenseNature {
		natureHTML += fmt.Sprintf(`<div class="status-row">
			<span class="label">%s</span>
			<span class="value">%s</span>
		</div>`, id, name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, html,
		store.Count(),
		lastSyncStr,
		cfg.Sync.IntervalMinutes,
		cfg.Sync.QueueSize,
		targetsHTML,
		natureHTML,
	)
}
