(function() {
  var REFRESH_MS = 3000;
  var currentPage = 'overview';

  function fmtTag(cls, text) {
    return '<span class="tag tag-' + cls + '">' + text + '</span>';
  }
  function escapeHtml(s) {
    if (s == null) return '';
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
  function checkAuth(r) {
    if (r.status === 401) { window.location.href = '/login'; return false; }
    return true;
  }

  function switchPage(page) {
    currentPage = page;
    document.querySelectorAll('.nav-tab').forEach(function(el) {
      el.classList.toggle('active', el.dataset.page === page);
    });
    var container = document.getElementById('pageContainer');
    container.innerHTML = '';
    switch(page) {
      case 'overview': renderOverviewPage(container); break;
      case 'rules': renderRulesPage(container); break;
      case 'webhooks': renderWebhooksPage(container); break;
    }
    refresh();
  }

  function renderOverviewPage(container) {
    container.innerHTML = '<div class="grid">' +
      '<section class="card card-status"><h2>服务状态</h2><div class="kv-list">' +
        '<div class="kv"><span class="k">版本号</span><span class="v" id="version">-</span></div>' +
        '<div class="kv"><span class="k">总末端节点</span><span class="v" id="totalLeaf">-</span></div>' +
        '<div class="kv"><span class="k">同步状态</span><span class="v" id="syncState">-</span></div>' +
        '<div class="kv"><span class="k">上次同步</span><span class="v" id="lastSync">-</span></div>' +
        '<div class="kv"><span class="k">同步间隔</span><span class="v" id="interval">-</span></div>' +
        '<div class="kv"><span class="k">队列容量</span><span class="v" id="queueSize">-</span></div>' +
      '</div></section>' +
      '<section class="card card-resource"><h2>资源占用</h2><div class="kv-list">' +
        '<div class="kv"><span class="k">内存</span><span class="v" id="memory">-</span></div>' +
        '<div class="kv"><span class="k">协程数</span><span class="v" id="goroutines">-</span></div>' +
      '</div></section>' +
      '<section class="card card-targets"><h2>预算目标</h2><div class="kv-list" id="targetsList"><div class="kv empty">加载中...</div></div></section>' +
      '<section class="card card-history"><h2>最近处理</h2><div class="history-list" id="historyList"><div class="empty">暂无处理记录</div></div></section>' +
      '<section class="card card-actions"><h2>操作</h2><div class="btn-group">' +
        '<button class="btn btn-primary" id="btnSync" onclick="doSync()">手动同步</button>' +
        '<a class="btn btn-secondary" href="/api/status" target="_blank">状态 API</a>' +
      '</div><div class="sync-msg" id="syncMsg"></div></section>' +
    '</div>';
  }

  function renderRulesPage(container) {
    container.innerHTML = '<div class="page-rules" id="rulesContainer"><div class="loading">加载中...</div></div>';
    loadRules();
  }

  function renderWebhooksPage(container) {
    container.innerHTML = '<div class="page-webhooks" id="webhooksContainer"><div class="loading">加载中...</div></div>';
    loadWebhooks();
  }

  function loadRules() {
    fetch('/api/webhooks').then(function(r) {
      if (!checkAuth(r)) return;
      return r.json();
    }).then(function(list) {
      var container = document.getElementById('rulesContainer');
      container.innerHTML = '';
      if (!list || list.length === 0) {
        container.innerHTML = '<div class="empty">未配置 webhook</div>';
        return;
      }
      list.forEach(function(wh) {
        var section = document.createElement('div');
        section.className = 'rules-webhook-section';
        var rulesPath = wh.rules || ('rules/' + wh.key + '.json');
        section.innerHTML = '<h3 class="webhook-title">' + escapeHtml(wh.key) +
          '<span class="webhook-meta">' + escapeHtml(rulesPath) + '</span></h3>' +
          '<div class="rules-loading" id="rules-' + wh.key + '">加载中...</div>';
        container.appendChild(section);
        fetch('/api/rules/' + wh.key).then(function(r) {
          if (!checkAuth(r)) return;
          return r.json();
        }).then(function(cfg) {
          renderRulesForWebhook(wh.key, cfg);
        }).catch(function(e) {
          document.getElementById('rules-' + wh.key).innerHTML = '<div class="empty">加载失败: ' + escapeHtml(String(e)) + '</div>';
        });
      });
    }).catch(function(e) {
      console.warn('webhooks fetch failed', e);
    });
  }

  function renderRulesForWebhook(key, cfg) {
    var el = document.getElementById('rules-' + key);
    if (!cfg || !cfg.targets || cfg.targets.length === 0) {
      el.innerHTML = '<div class="empty">未配置规则 target</div>';
      return;
    }
    el.innerHTML = cfg.targets.map(function(t) {
      var stepsHtml = (t.steps || []).map(function(s, si) {
        var type = s.action ? 'action' : (s.when ? 'condition' : 'unknown');
        var icon = type === 'action' ? '&#9881;' : (type === 'condition' ? '&#128260;' : '&#10067;');
        var content = '';
        if (s.action) content += '<div class="step-line step-action">action: <code>' + escapeHtml(s.action) + '</code></div>';
        if (s.when) content += '<div class="step-line step-when">when: <code>' + escapeHtml(s.when) + '</code></div>';
        if (s.then) content += '<div class="step-line step-then">then: <code>' + escapeHtml(s.then) + '</code></div>';
        if (s.reason) content += '<div class="step-line step-reason">reason: ' + escapeHtml(s.reason) + '</div>';
        return '<div class="step-item step-' + type + '">' +
          '<div class="step-num">' + (si + 1) + '</div>' +
          '<div class="step-body">' +
            '<div class="step-icon">' + icon + '</div>' +
            '<div class="step-main">' +
              '<div class="step-desc">' + escapeHtml(s.description || '') + '</div>' +
              content +
            '</div>' +
          '</div>' +
        '</div>';
      }).join('');
      return '<div class="rule-target-card">' +
        '<div class="target-header">' +
          '<span class="target-name">' + escapeHtml(t.name) + '</span>' +
          '<code class="target-id">' + escapeHtml(t.id) + '</code>' +
        '</div>' +
        '<div class="step-list">' + stepsHtml + '</div>' +
      '</div>';
    }).join('');
  }

  function loadWebhooks() {
    fetch('/api/webhooks').then(function(r) {
      if (!checkAuth(r)) return;
      return r.json();
    }).then(function(list) {
      var el = document.getElementById('webhooksContainer');
      if (!list || list.length === 0) {
        el.innerHTML = '<div class="empty">未配置 webhook</div>';
        return;
      }
      el.innerHTML = '<table class="data-table">' +
        '<thead><tr><th>Key</th><th>Sign Key</th><th>Rules 文件</th><th>预算包</th></tr></thead>' +
        '<tbody>' + list.map(function(wh) {
          var targets = (wh.targets || []).map(function(t) {
            return escapeHtml(t.name || t.id);
          }).join('、');
          return '<tr>' +
            '<td><strong>' + escapeHtml(wh.key) + '</strong></td>' +
            '<td><code class="muted">' + escapeHtml(wh.sign_key) + '</code></td>' +
            '<td>' + escapeHtml(wh.rules || 'rules/' + wh.key + '.json') + '</td>' +
            '<td>' + (targets || '-') + '</td>' +
          '</tr>';
        }).join('') + '</tbody></table>';
    }).catch(function(e) {
      console.warn('webhooks fetch failed', e);
    });
  }

  function renderTargets(targets) {
    var el = document.getElementById('targetsList');
    if (!el) return;
    if (!targets || targets.length === 0) {
      el.innerHTML = '<div class="kv empty">暂无预算目标</div>';
      return;
    }
    el.innerHTML = targets.map(function(t) {
      return '<div class="kv"><span class="k">' + escapeHtml(t.name) + '</span><span class="v">' + fmtTag('ok', t.count + ' 末端节点') + '</span></div>';
    }).join('');
  }

  function renderHistory(list) {
    var el = document.getElementById('historyList');
    if (!el) return;
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

  function refresh() {
    if (currentPage === 'overview') {
      fetch('/api/status').then(function(r) {
        if (!checkAuth(r)) return;
        return r.json();
      }).then(function(d) {
        if (!d) return;
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
      }).catch(function(e) {
        console.warn('status fetch failed', e);
      });

      fetch('/api/history').then(function(r) {
        if (!checkAuth(r)) return;
        return r.json();
      }).then(function(d) {
        if (!d) return;
        renderHistory(d);
      }).catch(function(e) {
        console.warn('history fetch failed', e);
      });
    }
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
      .then(function(r) {
        if (r.status === 401) { window.location.href = '/login'; return null; }
        return r.json();
      })
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

  document.getElementById('navTabs').addEventListener('click', function(e) {
    if (e.target.classList.contains('nav-tab')) {
      e.preventDefault();
      switchPage(e.target.dataset.page);
    }
  });

  switchPage('overview');
  setInterval(refresh, REFRESH_MS);
})();
