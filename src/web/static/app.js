const { createApp, ref, onMounted, onUnmounted } = Vue

const app = createApp({
  setup() {
    // 状态
    const currentPage = ref('overview')
    const version = ref('-')
    const totalLeafCount = ref(0)
    const isSyncing = ref(false)
    const lastSyncAt = ref('未同步')
    const intervalMinutes = ref(0)
    const queueSize = ref(0)
    const queuePending = ref(0)
    const memoryMB = ref(0)
    const goroutines = ref(0)
    const targets = ref([])
    const history = ref([])
    const webhooks = ref([])
    const rules = ref({})
    const syncing = ref(false)
    const syncMsg = ref('')
    const metrics = ref({})

    // 编辑器状态
    const editMode = ref(null)
    const editDraft = ref(null)
    const editMsg = ref('')
    const editSaving = ref(false)

    let refreshTimer = null

    function checkAuth(r) {
      if (r.status === 401) {
        window.location.href = '/login'
        return false
      }
      return true
    }

    function switchPage(page) {
      currentPage.value = page
      if (page === 'rules') loadRules()
      if (page === 'webhooks') loadWebhooks()
    }

    async function refresh() {
      try {
        const r = await fetch('/api/status')
        if (!checkAuth(r)) return
        const d = await r.json()
        if (!d) return
        version.value = d.version || '-'
        totalLeafCount.value = d.total_leaf_count || 0
        isSyncing.value = d.is_syncing
        lastSyncAt.value = d.last_sync_at || '未同步'
        intervalMinutes.value = d.interval_minutes || 0
        queueSize.value = d.queue?.capacity || 0
        queuePending.value = d.queue?.pending || 0
        memoryMB.value = d.memory_mb || 0
        goroutines.value = d.goroutines || 0
        targets.value = d.targets || []
        metrics.value = d.metrics || {}
      } catch (e) {
        console.warn('status fetch failed', e)
      }
      try {
        const r = await fetch('/api/history')
        if (!checkAuth(r)) return
        const d = await r.json()
        if (d) history.value = d
      } catch (e) {
        console.warn('history fetch failed', e)
      }
    }

    async function loadWebhooks() {
      try {
        const r = await fetch('/api/webhooks')
        if (!checkAuth(r)) return
        const list = await r.json()
        webhooks.value = list || []
      } catch (e) {
        console.warn('webhooks fetch failed', e)
      }
    }

    async function loadRules() {
      await loadWebhooks()
      for (const wh of webhooks.value) {
        try {
          const r = await fetch('/api/rules/' + wh.key)
          if (!checkAuth(r)) return
          const cfg = await r.json()
          rules.value[wh.key] = cfg
        } catch (e) {
          console.warn('rules fetch failed', wh.key, e)
          rules.value[wh.key] = null
        }
      }
    }

    async function doSync() {
      const p = prompt('输入同步密码')
      if (p === null) return
      syncing.value = true
      syncMsg.value = '同步中...'
      try {
        let url = '/api/sync'
        if (p) url += '?password=' + encodeURIComponent(p)
        const r = await fetch(url, { method: 'POST' })
        if (r.status === 401) { window.location.href = '/login'; return }
        const d = await r.json()
        syncMsg.value = d.message || JSON.stringify(d)
        refresh()
      } catch (e) {
        syncMsg.value = '失败: ' + e
      } finally {
        syncing.value = false
      }
    }

    function formatTimestamp(ts) {
      if (!ts || ts === 0) return '未同步'
      return new Date(ts * 1000).toLocaleString()
    }

    // ========== 规则编辑器 ==========

    function startEdit(key) {
      editMode.value = key
      editDraft.value = JSON.parse(JSON.stringify(rules.value[key]))
      editMsg.value = ''
    }

    function cancelEdit() {
      editMode.value = null
      editDraft.value = null
      editMsg.value = ''
    }

    function addTarget() {
      editDraft.value.targets.push({
        id: '',
        name: '新预算包',
        steps: [{ description: '按费用明细拆分', action: 'split_detail' }]
      })
    }

    function removeTarget(ti) {
      editDraft.value.targets.splice(ti, 1)
    }

    function addStep(ti) {
      editDraft.value.targets[ti].steps.push({ description: '' })
    }

    function removeStep(ti, si) {
      editDraft.value.targets[ti].steps.splice(si, 1)
    }

    function moveStep(ti, si, dir) {
      const steps = editDraft.value.targets[ti].steps
      const ni = si + dir
      if (ni < 0 || ni >= steps.length) return
      const tmp = steps[si]
      steps[si] = steps[ni]
      steps[ni] = tmp
    }

    async function saveRules(key) {
      editSaving.value = true
      editMsg.value = ''
      try {
        const r = await fetch('/api/rules/' + key, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(editDraft.value)
        })
        if (!checkAuth(r)) return
        const d = await r.json()
        if (r.ok) {
          rules.value[key] = JSON.parse(JSON.stringify(editDraft.value))
          editMsg.value = '✓ 保存成功，规则引擎已热重载'
          setTimeout(() => { editMsg.value = '' }, 3000)
        } else {
          editMsg.value = '✗ ' + (d.error || '保存失败')
        }
      } catch (e) {
        editMsg.value = '✗ 网络错误: ' + e
      } finally {
        editSaving.value = false
      }
    }

    onMounted(() => {
      refresh()
      refreshTimer = setInterval(refresh, 3000)
    })

    onUnmounted(() => {
      if (refreshTimer) clearInterval(refreshTimer)
    })

    return {
      currentPage, version, totalLeafCount, isSyncing, lastSyncAt,
      intervalMinutes, queueSize, queuePending, memoryMB, goroutines,
      targets, history, webhooks, rules, syncing, syncMsg, metrics,
      switchPage, doSync, formatTimestamp,
      editMode, editDraft, editMsg, editSaving,
      startEdit, cancelEdit, addTarget, removeTarget,
      addStep, removeStep, moveStep, saveRules
    }
  }
})

app.mount('#app')
