(function() {
  var REFRESH_MS = 3000;

  function fmtTag(cls, text) {
    return '<span class="tag tag-' + cls + '">' + text + '</span>';
  }

  function renderTargets(targets) {
    var el = document.getElementById('targetsList');
    if (!targets || targets.length === 0) {
      el.innerHTML = '<div class="kv empty">暂无预算目标</div>';
      return;
    }
    el.innerHTML = targets.map(function(t) {
      return '<div class="kv"><span class="k">' + escapeHtml(t.name) + '</span><span class="v">' + fmtTag('ok', t.count + ' 末端节点') + '</span></div>';
    }).join('');
  }

  function renderNature(map) {
    var el = document.getElementById('natureList');
    var keys = Object.keys(map || {});
    if (keys.length === 0) {
      el.innerHTML = '<div class="kv empty">暂无数据</div>';
      return;
    }
    el.innerHTML = keys.map(function(k) {
      return '<div class="kv"><span class="k">' + escapeHtml(k) + '</span><span class="v">' + escapeHtml(map[k]) + '</span></div>';
    }).join('');
  }

  function renderHistory(list) {
    var el = document.getElementById('historyList');
    if (!list || list.length === 0) {
      el.innerHTML = '<div class="empty">暂无处理记录</div>';
      return;
    }
    el.innerHTML = list.map(function(h) {
      var cls = h.action === 'accept' ? 'accept' : 'refuse';
      return '<div class="history-item">' +
        '<span class="time">' + escapeHtml(h.time) + '</span>' +
        '<span class="code">' + escapeHtml(h.code) + '</span>' +
        '<span class="action">' + fmtTag(cls, h.action) + '</span>' +
        '<span class="comment">' + escapeHtml(h.comment || '') + '</span>' +
      '</div>';
    }).join('');
  }

  function escapeHtml(s) {
    if (s == null) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function refresh() {
    fetch('/api/status').then(function(r) { return r.json(); }).then(function(d) {
      document.getElementById('version').textContent = d.version || '-';
      document.getElementById('headerVersion').textContent = 'v' + (d.version || '-');
      var tlc = d.total_leaf_count || 0;
      document.getElementById('totalLeaf').textContent = tlc > 0 ? tlc + ' 节点' : '-';

      var syncEl = document.getElementById('syncState');
      if (d.is_syncing) {
        syncEl.innerHTML = fmtTag('warn', '同步中...');
      } else if (d.last_sync_at) {
        syncEl.innerHTML = fmtTag('ok', '同步完成');
      } else {
        syncEl.textContent = '未同步';
      }

      document.getElementById('lastSync').textContent = d.last_sync_at || '未同步';
      document.getElementById('interval').textContent = (d.interval_minutes || '-') + ' 分钟';
      document.getElementById('queueSize').textContent = d.queue_size || '-';
      document.getElementById('memory').textContent = (d.memory_mb || 0) + ' MB';
      document.getElementById('goroutines').textContent = d.goroutines || 0;

      renderTargets(d.targets);
      renderNature(d.expense_nature);
    }).catch(function(e) {
      console.warn('status fetch failed', e);
    });

    fetch('/api/history').then(function(r) { return r.json(); }).then(function(d) {
      renderHistory(d);
    }).catch(function(e) {
      console.warn('history fetch failed', e);
    });
  }

  window.doSync = function() {
    var btn = document.getElementById('btnSync');
    var msg = document.getElementById('syncMsg');
    btn.disabled = true;
    msg.textContent = '同步中...';

    var p = prompt('输入同步密码');
    var url = '/api/sync';
    if (p) url += '?password=' + encodeURIComponent(p);

    fetch(url, { method: 'POST' })
      .then(function(r) { return r.json(); })
      .then(function(d) {
        msg.textContent = d.message || JSON.stringify(d);
        btn.disabled = false;
        refresh();
      })
      .catch(function(e) {
        msg.textContent = '失败: ' + e;
        btn.disabled = false;
      });
  };

  refresh();
  setInterval(refresh, REFRESH_MS);
})();
