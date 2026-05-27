package main

import (
	"budget/src/budget"
	"budget/src/consumer"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func handleStatus(w http.ResponseWriter, r *http.Request, store *budget.Store) {
	lastSync := store.UpdatedAt()
	lastSyncStr := ""
	if !lastSync.IsZero() {
		lastSyncStr = lastSync.Format(time.RFC3339)
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	writeJSON(w, 200, map[string]interface{}{
		"status":        "ok",
		"version":       version,
		"count":         store.Count(),
		"sync_progress": store.SyncProgress(),
		"is_syncing":    syncing.Load(),
		"last_sync_at":  lastSyncStr,
		"memory_mb":     bToMB(m.Alloc),
		"goroutines":    runtime.NumGoroutine(),
	})
}

func handleHistory(w http.ResponseWriter, r *http.Request, checker *consumer.Checker) {
	writeJSON(w, 200, checker.GetHistory())
}

func bToMB(b uint64) uint64 { return b / 1024 / 1024 }

func handleHome(w http.ResponseWriter, r *http.Request, cfg *Config, store *budget.Store) {
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
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f5f5f5;color:#333;padding:20px}
h1{font-size:18px;margin-bottom:16px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}
.card{background:#fff;border-radius:8px;padding:16px;box-shadow:0 1px 3px rgba(0,0,0,.1)}
.card h2{font-size:13px;color:#999;margin-bottom:10px;border-bottom:1px solid #f0f0f0;padding-bottom:6px}
.row{display:flex;justify-content:space-between;padding:5px 0;font-size:13px;border-bottom:1px solid #fafafa}
.row:last-child{border-bottom:none}
.label{color:#888}
.value{font-weight:500}
.tag{display:inline-block;padding:1px 6px;border-radius:3px;font-size:11px}
.tag-ok{background:#f6ffed;color:#52c41a;border:1px solid #b7eb8f}
.tag-accept{background:#f6ffed;color:#52c41a}
.tag-refuse{background:#fff2f0;color:#ff4d4f}
.tag-warn{background:#fffbe6;color:#faad14;border:1px solid #ffe58f}
.btn{padding:6px 14px;border:none;border-radius:4px;cursor:pointer;font-size:12px;text-decoration:none;display:inline-block}
.btn-primary{background:#1677ff;color:#fff}
.btn-primary:hover{background:#4096ff}
.actions{display:flex;gap:8px;margin-top:10px}
.history-list{max-height:360px;overflow-y:auto;font-size:12px}
.history-item{display:flex;gap:8px;padding:4px 0;border-bottom:1px solid #fafafa;align-items:center}
.history-item .time{color:#999;width:55px;flex-shrink:0}
.history-item .code{width:100px;flex-shrink:0;font-family:monospace}
.history-item .action{width:50px;flex-shrink:0}
.history-item .comment{color:#666;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
</style>
</head>
<body>
<h1>合思预算校验服务 <span style="font-size:12px;color:#999;font-weight:normal">v%s</span></h1>
<div class="grid">
  <div class="card">
    <h2>服务状态</h2>
    <div id="status">
      <div class="row"><span class="label">版本号</span><span class="value" id="version">-</span></div>
      <div class="row"><span class="label">缓存条目</span><span class="value" id="count">-</span></div>
      <div class="row"><span class="label">已拉取</span><span class="value" id="fetched">-</span></div>
      <div class="row"><span class="label">同步状态</span><span class="value" id="syncState">-</span></div>
      <div class="row"><span class="label">上次同步</span><span class="value" id="lastSync">-</span></div>
      <div class="row"><span class="label">同步间隔</span><span class="value">%d 分钟</span></div>
      <div class="row"><span class="label">队列容量</span><span class="value">%d</span></div>
    </div>
  </div>
  <div class="card">
    <h2>资源占用</h2>
    <div class="row"><span class="label">内存</span><span class="value" id="memory">-</span></div>
    <div class="row"><span class="label">协程数</span><span class="value" id="goroutines">-</span></div>
  </div>
  <div class="card">
    <h2>预算目标</h2>%s
  </div>
  <div class="card">
    <h2>费用性质</h2>%s
  </div>
  <div class="card" style="grid-column:1/-1">
    <h2>最近处理</h2>
    <div class="history-list" id="history">
      <div style="color:#999;text-align:center;padding:20px">暂无处理记录</div>
    </div>
  </div>
  <div class="card" style="grid-column:1/-1">
    <div class="actions">
      <button class="btn btn-primary" onclick="doSync()">手动同步</button>
      <a class="btn btn-primary" href="/api/status" target="_blank">状态 API</a>
    </div>
  </div>
</div>
<script>
function refresh(){
  fetch('/api/status').then(function(r){return r.json()}).then(function(d){
    document.getElementById('version').textContent=d.version||'-';
    document.getElementById('count').textContent=d.count;
    var sp=d.sync_progress||0;
    var spText=sp>0?sp+' 条':'-';
    document.getElementById('fetched').textContent=spText;
    var syncEl=document.getElementById('syncState');
    if(d.is_syncing){
      syncEl.innerHTML='<span class="tag tag-warn">同步中...</span>';
    }else if(d.last_sync_at){
      syncEl.innerHTML='<span class="tag tag-ok">同步完成</span>';
    }else{
      syncEl.textContent='未同步';
    }
    document.getElementById('lastSync').textContent=d.last_sync_at||'未同步';
    document.getElementById('memory').textContent=d.memory_mb+' MB';
    document.getElementById('goroutines').textContent=d.goroutines;
  });
  fetch('/api/history').then(function(r){return r.json()}).then(function(d){
    var el=document.getElementById('history');
    if(!d||d.length===0){el.innerHTML='<div style="color:#999;text-align:center;padding:20px">暂无处理记录</div>';return}
    el.innerHTML=d.map(function(h){
      var cls=h.action==='accept'?'tag-accept':'tag-refuse';
      return '<div class="history-item"><span class="time">'+h.time+'</span><span class="code">'+h.code+'</span><span class="action"><span class="tag '+cls+'">'+h.action+'</span></span><span class="comment">'+h.comment+'</span></div>';
    }).join('');
  });
}
refresh();
setInterval(refresh,3000);
function doSync(){
  var p=prompt('输入同步密码');
  fetch('/api/sync'+(p?'?password='+encodeURIComponent(p):''),{method:'POST'})
    .then(function(r){return r.json()})
    .then(function(d){alert(d.message||JSON.stringify(d))})
    .catch(function(e){alert('失败: '+e)});
}
</script>
</body>
</html>`

	targetsHTML := ""
	for _, tree := range store.Trees() {
		count := store.GetTreeNodeCount(tree.ID)
		targetsHTML += fmt.Sprintf(`<div class="row"><span class="label">%s</span><span class="value"><span class="tag tag-ok">%d 节点</span></span></div>`, tree.Name, count)
	}

	natureHTML := ""
	for id, name := range consumer.ExpenseNature {
		natureHTML += fmt.Sprintf(`<div class="row"><span class="label">%s</span><span class="value">%s</span></div>`, id, name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, html, version, cfg.Sync.IntervalMinutes, cfg.Sync.QueueSize, targetsHTML, natureHTML)
}
