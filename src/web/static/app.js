const { createApp, ref, computed, reactive, onMounted, onUnmounted } = Vue

// Resource health standard values (hardcoded in frontend)
const MEMORY_STD = 30      // MB
const GOROUTINE_STD = 20   // count

// Gradient color stops for health ring
const COLOR_STOPS = [
  { pct: 0,   r: 82,  g: 196, b: 26  },  // bright green
  { pct: 25,  r: 115, g: 209, b: 61  },  // medium green
  { pct: 50,  r: 250, g: 173, b: 20  },  // yellow
  { pct: 65,  r: 255, g: 122, b: 69  },  // orange
  { pct: 80,  r: 255, g: 77,  b: 79  },  // red
  { pct: 90,  r: 207, g: 19,  b: 34  },  // dark red
  { pct: 100, r: 255, g: 77,  b: 79  },  // red
]

/**
 * Interpolate ring color based on percentage.
 * 0-50%: green range, 50-90%: yellow→orange→red range, 90-100%: red range, >100%: purple
 */
function ringColor(pct) {
  if (pct > 100) return '#722ed1'
  for (let i = 0; i < COLOR_STOPS.length - 1; i++) {
    const a = COLOR_STOPS[i], b = COLOR_STOPS[i + 1]
    if (pct >= a.pct && pct <= b.pct) {
      const t = (pct - a.pct) / (b.pct - a.pct)
      return `rgb(${Math.round(a.r + (b.r - a.r) * t)},${Math.round(a.g + (b.g - a.g) * t)},${Math.round(a.b + (b.b - a.b) * t)})`
    }
  }
  return '#722ed1'
}

const CIRCUMFERENCE = 2 * Math.PI * 36  // r=36 → 226.19

const app = createApp({
  setup() {
    // Page navigation
    const currentPage = ref('overview')
    function switchPage(p) { currentPage.value = p }

    // Overview data
    const version = ref('')
    const totalLeafCount = ref(0)
    const feeTypeCount = ref(0)
    const isSyncing = ref(false)
    const lastSyncAt = ref('')
    const intervalMinutes = ref(0)
    const queuePending = ref(0)
    const queueSize = ref(0)
    const memoryMB = ref(0)
    const goroutines = ref(0)
    const targets = ref([])
    const metrics = ref({ checks: {}, syncs: { success: 0, error: 0 }, last_sync_timestamp: 0 })
    const history = ref([])

    // Sync
    const syncing = ref(false)
    const syncMsg = ref('')

    // Rules
    const webhooks = ref([])
    const rules = ref({})

    // Editor
    const editMode = ref(null)
    const editDraft = ref(null)
    const editSaving = ref(false)
    const editMsg = ref('')

    // Create Webhook modal
    const showCreateModal = ref(false)
    const createForm = reactive({ key: '', sign_key: '' })
    const createSaving = ref(false)
    const createMsg = ref('')

    // Password verification modal (shared for save rules and create webhook)
    const showPasswordModal = ref(false)
    const verifyPassword = ref('')
    const verifySaving = ref(false)
    const verifyMsg = ref('')
    let pendingAction = null // { type: 'saveRules', key } or { type: 'createWebhook' }

    // Ring computations
    const memoryPct = computed(() => Math.round((memoryMB.value / MEMORY_STD) * 100))
    const goroutinePct = computed(() => Math.round((goroutines.value / GOROUTINE_STD) * 100))
    const memoryColor = computed(() => ringColor(memoryPct.value))
    const goroutineColor = computed(() => ringColor(goroutinePct.value))

    // Queue progress
    const queuePct = computed(() => queueSize.value > 0 ? Math.round((queuePending.value / queueSize.value) * 100) : 0)
    const queueBarClass = computed(() => {
      const p = queuePct.value
      if (p >= 90) return 'queue-critical'
      if (p >= 70) return 'queue-warn'
      return 'queue-ok'
    })

    // Sign key masking and copy
    function maskKey(key) {
      if (!key || key.length <= 8) return key
      return key.slice(0, 4) + '****' + key.slice(-4)
    }
    function copyText(text) {
      navigator.clipboard.writeText(text).catch(() => {})
    }

    let refreshTimer = null

    // Format ISO timestamp to HH:MM:SS
    function formatTimestamp(ts) {
      if (!ts) return '-'
      if (typeof ts === 'number') {
        // unix seconds
        const d = new Date(ts * 1000)
        return d.toLocaleTimeString('zh-CN', { hour12: false })
      }
      const d = new Date(ts)
      if (isNaN(d)) return ts
      return d.toLocaleTimeString('zh-CN', { hour12: false })
    }

    function formatSyncTime(s) {
      if (!s) return '-'
      const d = new Date(s)
      if (isNaN(d)) return s
      return d.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
    }

    // Fetch status
    async function refresh() {
      try {
        const r = await fetch('/api/status')
        const d = await r.json()
        version.value = d.version || ''
        totalLeafCount.value = d.total_leaf_count || 0
        feeTypeCount.value = d.fee_type_count || 0
        isSyncing.value = !!d.is_syncing
        lastSyncAt.value = formatSyncTime(d.last_sync_at)
        intervalMinutes.value = d.interval_minutes || 0
        memoryMB.value = d.memory_mb || 0
        goroutines.value = d.goroutines || 0
        queuePending.value = d.queue?.pending || 0
        queueSize.value = d.queue?.capacity || 0
        targets.value = d.targets || []
        if (d.metrics) metrics.value = d.metrics
      } catch (e) { /* ignore */ }
    }

    // Fetch history
    async function refreshHistory() {
      try {
        const r = await fetch('/api/history')
        history.value = await r.json()
      } catch (e) { /* ignore */ }
    }

    // Fetch webhook configs
    async function loadWebhooks() {
      try {
        const r = await fetch('/api/webhooks')
        webhooks.value = await r.json()
      } catch (e) { webhooks.value = [] }
    }

    // Fetch rules for each webhook
    async function loadAllRules() {
      for (const wh of webhooks.value) {
        try {
          const r = await fetch('/api/rules/' + wh.key)
          if (r.ok) {
            const data = await r.json()
            rules.value[wh.key] = data
          }
        } catch (e) { /* skip */ }
      }
      // Force reactivity
      rules.value = { ...rules.value }

      // Default: select first webhook and expand all targets
      if (webhooks.value.length > 0 && !selectedWebhook.value) {
        selectedWebhook.value = webhooks.value[0].key
      }
      // Expand all targets by default
      if (selectedWebhook.value && rules.value[selectedWebhook.value]?.targets) {
        for (const t of rules.value[selectedWebhook.value].targets) {
          expandedTargets.value[t.id] = true
        }
      }
    }

    // Manual sync
    async function doSync() {
      syncing.value = true
      syncMsg.value = ''
      try {
        const r = await fetch('/api/sync', { method: 'POST' })
        const d = await r.json()
        syncMsg.value = d.ok ? '同步完成' : ('失败: ' + (d.error || '未知错误'))
        await refresh()
      } catch (e) {
        syncMsg.value = '请求失败: ' + e.message
      } finally {
        syncing.value = false
        setTimeout(() => { syncMsg.value = '' }, 5000)
      }
    }

    // Editor functions
    function startEdit(key) {
      editMode.value = key
      editMsg.value = ''
      const r = rules.value[key]
      if (r) {
        editDraft.value = JSON.parse(JSON.stringify(r))
        // 解析 when 表达式为三段式
        if (editDraft.value.targets) {
          for (const t of editDraft.value.targets) {
            if (t.steps) {
              for (const s of t.steps) {
                const parsed = parseWhenExpr(s.when)
                s._whenField = parsed.field
                s._whenOp = parsed.op
                s._whenValue = parsed.value
              }
            }
          }
        }
      }
    }

    function cancelEdit() {
      editMode.value = null
      editDraft.value = null
      editMsg.value = ''
    }

    function addTarget() {
      if (!editDraft.value) return
      if (!editDraft.value.targets) editDraft.value.targets = []
      editDraft.value.targets.push({
        id: '', name: '', when: '',
        steps: [],
      })
    }

    function removeTarget(idx) {
      editDraft.value.targets.splice(idx, 1)
    }

    function addStep(targetIdx) {
      editDraft.value.targets[targetIdx].steps.push({
        action: '', when: '', then: '', reason: '', description: '',
        _whenField: '', _whenOp: '==', _whenValue: '',
      })
    }

    function removeStep(targetIdx, stepIdx) {
      editDraft.value.targets[targetIdx].steps.splice(stepIdx, 1)
    }

    function moveStep(targetIdx, stepIdx, dir) {
      const steps = editDraft.value.targets[targetIdx].steps
      const newIdx = stepIdx + dir
      if (newIdx < 0 || newIdx >= steps.length) return
      const tmp = steps[stepIdx]
      steps[stepIdx] = steps[newIdx]
      steps[newIdx] = tmp
    }

    async function saveRules(key) {
      if (!editDraft.value) return
      // 弹出密码验证弹窗
      pendingAction = { type: 'saveRules', key }
      verifyPassword.value = ''
      verifyMsg.value = ''
      showPasswordModal.value = true
    }

    // Show create webhook confirmation (called from create modal)
    function showCreateConfirm() {
      if (!createForm.key.trim()) { createMsg.value = '请输入 Webhook Key'; return }
      if (!createForm.sign_key.trim()) { createMsg.value = '请输入 Sign Key'; return }
      // 弹出密码验证弹窗
      pendingAction = { type: 'createWebhook' }
      verifyPassword.value = ''
      verifyMsg.value = ''
      showPasswordModal.value = true
    }

    async function confirmPassword() {
      if (!verifyPassword.value.trim()) {
        verifyMsg.value = '请输入管理员密码'
        return
      }
      if (!pendingAction) return

      verifySaving.value = true
      verifyMsg.value = ''

      try {
        if (pendingAction.type === 'saveRules') {
          // 保存规则
          const r = await fetch('/api/rules/' + pendingAction.key, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              password: verifyPassword.value.trim(),
              config: editDraft.value,
            }),
          })
          const d = await r.json()
          if (r.ok && d.status === 'ok') {
            verifyMsg.value = '✓ 保存成功！'
            editMsg.value = '✓ 保存成功！重启后生效。'
            rules.value[pendingAction.key] = JSON.parse(JSON.stringify(editDraft.value))
            await refresh()
            setTimeout(() => {
              showPasswordModal.value = false
              verifyMsg.value = ''
            }, 800)
          } else {
            verifyMsg.value = d.error || '保存失败'
          }
        } else if (pendingAction.type === 'createWebhook') {
          // 创建 webhook
          createSaving.value = true
          createMsg.value = ''
          const r = await fetch('/api/webhooks', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              key: createForm.key.trim(),
              sign_key: createForm.sign_key.trim(),
              password: verifyPassword.value.trim(),
            }),
          })
          const d = await r.json()
          if (r.ok && d.status === 'ok') {
            verifyMsg.value = '✓ 创建成功！'
            createMsg.value = '✓ 创建成功！'
            await loadWebhooks()
            await loadAllRules()
            setTimeout(() => {
              showPasswordModal.value = false
              showCreateModal.value = false
              verifyMsg.value = ''
              createMsg.value = ''
              createForm.key = ''
              createForm.sign_key = ''
            }, 800)
          } else {
            verifyMsg.value = d.error || '创建失败'
          }
          createSaving.value = false
        }
      } catch (e) {
        verifyMsg.value = '请求失败: ' + e.message
      } finally {
        verifySaving.value = false
      }
    }

    // Create webhook - now handled by confirmPassword after password verification

    // Step detail descriptions (multi-line)
    const STEP_DETAILS = {
      split_detail:
        '将当前数据集中的每条记录，按其内嵌的明细（details）数组拆分为多条独立记录。\n' +
        '拆分后每条新记录会继承父记录的表单字段，并额外合并该明细行自身的表单字段（如费用类型档案等）。\n' +
        '拆分后的每条记录将独立参与后续所有步骤的校验，互不影响。',
      split_apportion:
        '将当前数据集中的每条记录，按其内嵌的分摊（apportions）数组拆分为多条独立记录。\n' +
        '拆分后每条新记录会继承父记录的表单字段，并额外合并该分摊行自身的表单字段。\n' +
        '拆分后的每条记录将独立参与后续所有步骤的校验，互不影响。',
      match_info_to_budget:
        '从预算树的根节点开始，逐层向下匹配。\n' +
        '每一层对应一个维度（如部门、项目、费用类型等），读取当前记录中该维度的字段值，\n' +
        '在当前层的节点中查找匹配项（支持沿父级链向上回溯查找）。\n' +
        '若某一层找不到匹配，返回错误并说明缺少哪个维度；\n' +
        '所有层均匹配成功，校验通过。',
    }
    const THEN_DETAILS = {
      pass: '该记录跳过后续所有步骤，直接判定为通过。',
      refuse: '该记录直接判定为拒绝，整个单据流程终止。',
      commit: '保留当前记录已累积的 unit（预算占用），并跳过后续所有非 split 类型的步骤。\n' +
             '如果后续还有 split_detail 或 split_apportion，仍会继续执行拆分。',
    }
    const hoveredStep = ref(null)

    // Rules page new states
    const selectedWebhook = ref(null)
    const expandedTargets = ref({})
    const showJsonPreview = ref(false)
    const showEditJsonPreview = ref(false)

    // Drag and drop states
    const dragSourceIndex = ref(null)
    const dragOverIndex = ref(null)

    // 切换 target 展开/折叠
    function toggleTarget(targetId) {
      expandedTargets.value[targetId] = !expandedTargets.value[targetId]
    }

    // 切换 JSON 预览
    function toggleJsonPreview() {
      showJsonPreview.value = !showJsonPreview.value
    }

    // 拖拽相关函数
    function onDragStart(event, targetIdx, stepIdx) {
      dragSourceIndex.value = { targetIdx, stepIdx }
      event.dataTransfer.effectAllowed = 'move'
      event.target.classList.add('dragging')
    }

    function onDragOver(event, targetIdx, stepIdx) {
      event.preventDefault()
      dragOverIndex.value = `${targetIdx}-${stepIdx}`
    }

    function onDrop(event, targetIdx, stepIdx) {
      event.preventDefault()
      if (!dragSourceIndex.value) return
      if (dragSourceIndex.value.targetIdx !== targetIdx) return // 不允许跨 target 拖拽

      const srcIdx = dragSourceIndex.value.stepIdx
      if (srcIdx === stepIdx) return

      const steps = editDraft.value.targets[targetIdx].steps
      const [moved] = steps.splice(srcIdx, 1)
      steps.splice(stepIdx, 0, moved)

      dragSourceIndex.value = null
      dragOverIndex.value = null
    }

    function onDragEnd(event) {
      event.target.classList.remove('dragging')
      dragSourceIndex.value = null
      dragOverIndex.value = null
    }

    // 判断是否是列表运算符
    function isListOp(op) {
      return op === 'in' || op === 'not in'
    }

    // 运算符变更时的处理
    function onWhenOpChange(s) {
      // 切换到非列表运算符时，清除值中的方括号
      if (!isListOp(s._whenOp) && s._whenValue) {
        s._whenValue = s._whenValue.replace(/^\[|\]$/g, '').replace(/^['"]|['"]$/g, '')
      }
      updateWhenExpr(s)
    }

    function stepDetail(s) {
      if (s.action) return STEP_DETAILS[s.action] || ''
      if (s.when) {
        const parts = [`条件: ${s.when}`]
        if (s.then) parts.push(`动作: ${s.then}`)
        if (s.reason) parts.push(`原因: ${s.reason}`)
        return parts.join('\n')
      }
      return ''
    }

    // 解析 when 表达式为三段式：字段、运算符、值
    function parseWhenExpr(expr) {
      if (!expr) return { field: '', op: '==', value: '' }

      // 运算符优先级（长的在前）
      const ops = ['not contains', 'not in', 'contains', 'in', '>=', '<=', '!=', '==', '>', '<']
      for (const op of ops) {
        const idx = expr.indexOf(op)
        if (idx > 0) {
          const field = expr.substring(0, idx).trim()
          let value = expr.substring(idx + op.length).trim()
          // 去掉外层引号
          value = value.replace(/^['"]|['"]$/g, '')
          // 去掉列表的方括号
          if (op === 'in' || op === 'not in') {
            value = value.replace(/^\[|\]$/g, '').replace(/^['"]|['"]$/g, '')
          }
          return { field, op, value }
        }
      }
      // 无法解析，返回原始值
      return { field: expr, op: '==', value: '' }
    }

    // 将三段式更新到 when 表达式
    function updateWhenExpr(s) {
      const field = (s._whenField || '').trim()
      const op = s._whenOp || '=='
      const value = (s._whenValue || '').trim()

      if (!field || !value) {
        s.when = ''
        return
      }

      // 需要引号的值（非数字）
      const needsQuote = (v) => isNaN(v) && !v.startsWith("'") && !v.startsWith('"')
      const quoteValue = (v) => needsQuote(v) ? `'${v}'` : v

      // contains/not contains 特殊格式
      if (op === 'contains') {
        s.when = `${field} contains ${quoteValue(value)}`
      } else if (op === 'not contains') {
        s.when = `${field} not contains ${quoteValue(value)}`
      } else if (op === 'in' || op === 'not in') {
        // 列表格式：值1, 值2, 值3 → ['值1', '值2', '值3']
        const items = value.split(',').map(v => quoteValue(v.trim())).filter(v => v)
        s.when = `${field} ${op} [${items.join(', ')}]`
      } else {
        s.when = `${field} ${op} ${quoteValue(value)}`
      }
    }

    // 步骤类型变更时的处理
    function onActionChange(s) {
      if (s.action) {
        // 选择动作类型时，清除条件相关字段
        s.when = ''
        s.then = ''
        s.reason = ''
        s._whenField = ''
        s._whenOp = '=='
        s._whenValue = ''
      }
    }

    // Lifecycle
    onMounted(async () => {
      await refresh()
      await refreshHistory()
      await loadWebhooks()
      await loadAllRules()
      refreshTimer = setInterval(() => {
        refresh()
        refreshHistory()
      }, 3000)
    })

    onUnmounted(() => {
      if (refreshTimer) clearInterval(refreshTimer)
    })

    return {
      // Page
      currentPage, switchPage,
      // Overview
      version, totalLeafCount, feeTypeCount, isSyncing, lastSyncAt, intervalMinutes,
      queuePending, queueSize, memoryMB, goroutines, targets, metrics, history,
      // Sync
      syncing, syncMsg, doSync,
      // Rules
      webhooks, rules,
      // Editor
      editMode, editDraft, editSaving, editMsg,
      startEdit, cancelEdit, addTarget, removeTarget,
      addStep, removeStep, moveStep, saveRules,
      // Create Webhook
      showCreateModal, createForm, createSaving, createMsg, showCreateConfirm,
      // Password verification
      showPasswordModal, verifyPassword, verifySaving, verifyMsg, confirmPassword,
      // Ring
      memoryPct, goroutinePct, memoryColor, goroutineColor,
      MEMORY_STD, GOROUTINE_STD, CIRCUMFERENCE,
      // Queue
      queuePct, queueBarClass,
      // Helpers
      formatTimestamp, stepDetail, hoveredStep,
      maskKey, copyText,
      // When editor
      parseWhenExpr, updateWhenExpr, onActionChange, isListOp, onWhenOpChange,
      // Rules page new features
      selectedWebhook, expandedTargets, showJsonPreview, showEditJsonPreview,
      toggleTarget, toggleJsonPreview,
      // Drag and drop
      dragSourceIndex, dragOverIndex, onDragStart, onDragOver, onDrop, onDragEnd,
    }
  }
})

app.mount('#app')
