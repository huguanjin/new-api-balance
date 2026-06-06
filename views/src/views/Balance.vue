<template>
  <div class="balance-container">
    <div class="header">
      <h2>余额管理系统</h2>
      <div class="actions">
        <el-button type="primary" @click="refreshAll" :loading="refreshing">刷新所有余额</el-button>
        <el-button type="info" @click="openImportDialog">导入渠道</el-button>
        <el-button @click="exportChannels">导出渠道</el-button>
        <el-button type="warning" @click="sendBalanceNotification" :loading="notifying">立即推送</el-button>
        <el-button @click="openNotificationDialog">通知设置</el-button>
        <el-button type="success" @click="openAddSite">添加站点</el-button>
        <el-button type="danger" @click="logout">退出</el-button>
      </div>
    </div>

    <el-alert
      v-if="notificationConfig.enabled"
      class="notification-alert"
      type="success"
      :closable="false"
      :title="notificationStatusTitle"
    />

    <div class="table-toolbar">
      <el-select v-model="statusFilter" placeholder="状态筛选" style="width: 140px">
        <el-option label="全部状态" value="all" />
        <el-option label="启用" value="1" />
        <el-option label="禁用" value="2" />
        <el-option label="未知" value="0" />
      </el-select>
      <span class="filter-summary">显示 {{ filteredSites.length }} / {{ sites.length }} 个渠道</span>
    </div>

    <el-table :data="filteredSites" style="width: 100%" border v-loading="loading">
      <el-table-column prop="channelId" label="渠道ID" width="90" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="channelStatusType(row.status)">{{ channelStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="name" label="站点名称" />
      <el-table-column prop="url" label="目标地址" />
      <el-table-column label="余额" width="200">
        <template #default="{ row }">
          <span v-if="row.balance !== undefined">
            <el-tag :type="row.balance > 0 ? 'success' : 'danger'">{{ row.balance.toFixed(4) }}</el-tag>
          </span>
          <span v-else-if="row.error" style="color: red">{{ row.error }}</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240">
        <template #default="{ row }">
          <el-tooltip content="刷新余额" placement="top">
          <el-button
            size="small"
            circle
            :icon="Refresh"
            :loading="isRefreshingSite(row)"
            @click="refreshSite(row)"
          />
          </el-tooltip>
          <el-button size="small" @click="editSite(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="deleteSite(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog :title="isEdit ? '编辑站点' : '添加站点'" v-model="dialogVisible" width="500px">
      <el-form :model="currentSite" label-width="110px">
        <el-form-item label="站点名称">
          <el-input v-model="currentSite.name" />
        </el-form-item>
        <el-form-item label="目标地址">
          <el-input v-model="currentSite.url" placeholder="如 https://api.xxx.com" />
        </el-form-item>
        <el-form-item label="适配">
          <el-select v-model="currentSite.adapter" clearable placeholder="默认（New API）" style="width: 100%">
            <el-option label="默认（New API）" value="" />
            <el-option label="青山" value="qingshan" />
            <el-option label="ePhone" value="ephone" />
          </el-select>
        </el-form-item>
        <el-form-item label="Token">
          <el-input v-model="currentSite.token" placeholder="Bearer sk-xxxx" />
        </el-form-item>
        <el-form-item label="User ID">
          <el-input v-model="currentSite.userId" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" @click="saveSite">保存</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog title="从上游导入渠道" v-model="importDialogVisible" width="680px">
      <el-alert
        class="import-alert"
        type="warning"
        :closable="false"
        title="导入会覆盖当前站点列表；地址重复时优先保留启用渠道，同状态保留上游返回中的最后一条。已有相同地址的 Token 和 User ID 会自动保留。"
      />
      <el-form :model="importForm" label-width="130px" v-loading="importConfigLoading">
        <el-form-item label="渠道接口 URL">
          <el-input
            v-model="importForm.url"
            type="textarea"
            :rows="3"
            placeholder="https://example.com/api/channel/?p=1&page_size=100&id_sort=false&tag_mode=false&status=enabled"
          />
        </el-form-item>
        <el-form-item label="Bearer Token">
          <el-input
            v-model="importForm.token"
            type="password"
            show-password
            placeholder="上游站点管理员 Token"
          />
        </el-form-item>
        <el-form-item label="new-api-user">
          <el-input v-model="importForm.userId" placeholder="如 1" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="importDialogVisible = false">取消</el-button>
          <el-button @click="saveImportConfig" :loading="savingImportConfig">保存配置</el-button>
          <el-button type="primary" @click="importChannels" :loading="importing">开始导入</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog title="余额通知设置" v-model="notificationDialogVisible" width="620px">
      <el-form
        :model="notificationConfig"
        label-width="130px"
        v-loading="notificationLoading"
      >
        <el-form-item label="启用通知">
          <el-switch v-model="notificationConfig.enabled" />
        </el-form-item>
        <el-form-item label="通知方式">
          <el-radio-group v-model="notificationConfig.notification_type">
            <el-radio value="feishu">飞书机器人</el-radio>
            <el-radio value="wework">企业微信机器人</el-radio>
          </el-radio-group>
        </el-form-item>
        <template v-if="notificationConfig.notification_type === 'feishu'">
          <el-form-item label="飞书 Webhook">
            <el-input
              v-model="notificationConfig.webhook_url"
              placeholder="飞书自定义机器人 Webhook URL"
            />
          </el-form-item>
          <el-form-item label="飞书签名密钥">
            <el-input
              v-model="notificationConfig.sign_key"
              placeholder="可选"
              type="password"
              show-password
            />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item label="企业微信 Webhook">
            <el-input
              v-model="notificationConfig.wework_webhook_url"
              placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxxx"
            />
          </el-form-item>
        </template>
        <el-form-item label="推送间隔">
          <el-input-number
            v-model="notificationConfig.interval_minutes"
            :min="1"
            :max="10080"
            :step="10"
          />
          <span class="form-suffix">分钟</span>
        </el-form-item>
        <el-form-item label="低余额阈值">
          <el-input-number
            v-model="notificationConfig.balance_threshold"
            :min="0"
            :precision="2"
            :step="100"
          />
          <span class="form-suffix">USD，0 表示推送全部</span>
        </el-form-item>
        <el-form-item label="上次尝试">
          <span>{{ formatDateTime(notificationConfig.last_attempt_at) }}</span>
        </el-form-item>
        <el-form-item label="上次成功">
          <span>{{ formatDateTime(notificationConfig.last_sent_at) }}</span>
        </el-form-item>
        <el-form-item v-if="notificationConfig.last_error" label="最近错误">
          <el-alert type="error" :closable="false" :title="notificationConfig.last_error" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="notificationDialogVisible = false">取消</el-button>
          <el-button @click="testNotification" :loading="testingNotification">测试通知</el-button>
          <el-button type="primary" @click="() => saveNotificationConfig()" :loading="savingNotification">
            保存设置
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import axios from 'axios'

const router = useRouter()
const sites = ref([])
const loading = ref(false)
const refreshing = ref(false)
const refreshingSiteKeys = ref(new Set())
const notifying = ref(false)
const importing = ref(false)
const importConfigLoading = ref(false)
const savingImportConfig = ref(false)
const statusFilter = ref('all')

const dialogVisible = ref(false)
const isEdit = ref(false)
const editIndex = ref(-1)
const currentSite = ref({ channelId: 0, status: 0, name: '', url: '', adapter: '', token: '', userId: '' })

const importDialogVisible = ref(false)
const importForm = ref({
  url: '',
  token: '',
  userId: '1'
})

const defaultNotificationConfig = () => ({
  enabled: false,
  notification_type: 'feishu',
  webhook_url: '',
  sign_key: '',
  wework_webhook_url: '',
  interval_minutes: 60,
  balance_threshold: 0,
  last_attempt_at: '',
  last_sent_at: '',
  last_error: ''
})

const notificationDialogVisible = ref(false)
const notificationLoading = ref(false)
const savingNotification = ref(false)
const testingNotification = ref(false)
const notificationConfig = ref(defaultNotificationConfig())

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const notificationTypeLabel = computed(() => (
  notificationConfig.value.notification_type === 'wework' ? '企业微信' : '飞书'
))

const notificationStatusTitle = computed(() => {
  const threshold = Number(notificationConfig.value.balance_threshold || 0)
  const thresholdText = threshold > 0 ? `，仅推送低于 ${threshold.toFixed(2)} USD 的渠道` : '，推送全部渠道'
  return `余额通知已启用，每 ${notificationConfig.value.interval_minutes} 分钟推送至 ${notificationTypeLabel.value}${thresholdText}`
})

const filteredSites = computed(() => {
  if (statusFilter.value === 'all') return sites.value
  const status = Number(statusFilter.value)
  return sites.value.filter(site => Number(site.status || 0) === status)
})

const fetchSites = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/sites', {
      headers: authHeaders()
    })
    sites.value = res.data.map(site => ({ ...site, balance: undefined, error: '' }))
  } catch (err) {
    if (err.response?.status === 401) {
      logout()
    } else {
      ElMessage.error('无法获取站点列表')
    }
  } finally {
    loading.value = false
  }
}

const fetchNotificationConfig = async () => {
  try {
    const res = await axios.get('/api/notification', {
      headers: authHeaders()
    })
    notificationConfig.value = {
      ...defaultNotificationConfig(),
      ...res.data
    }
  } catch (err) {
    if (err.response?.status === 401) {
      logout()
    } else {
      ElMessage.error('无法获取通知配置')
    }
  }
}

const syncToServer = async () => {
  try {
    const listToSave = sites.value.map(s => ({
      channelId: s.channelId || 0,
      status: s.status || 0,
      name: s.name,
      url: s.url,
      adapter: s.adapter || '',
      token: s.token,
      userId: s.userId
    }))
    await axios.post('/api/sites', listToSave, {
      headers: authHeaders()
    })
    await fetchSites()
    ElMessage.success('已保存配置')
  } catch (err) {
    ElMessage.error('保存失败')
  }
}

const openImportDialog = async () => {
  importDialogVisible.value = true
  importConfigLoading.value = true
  try {
    const res = await axios.get('/api/channels/import-config', {
      headers: authHeaders()
    })
    importForm.value = {
      url: res.data?.url || '',
      token: res.data?.token || '',
      userId: res.data?.userId || '1'
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '无法获取导入配置')
  } finally {
    importConfigLoading.value = false
  }
}

const importPayload = () => ({
  url: importForm.value.url.trim(),
  token: importForm.value.token.trim(),
  userId: importForm.value.userId.trim()
})

const validateImportForm = () => {
  const url = importForm.value.url.trim()
  const token = importForm.value.token.trim()
  if (!url) {
    ElMessage.error('请填写渠道接口 URL')
    return false
  }
  if (!token) {
    ElMessage.error('请填写 Bearer Token')
    return false
  }
  return true
}

const saveImportConfig = async () => {
  if (!validateImportForm()) return false

  savingImportConfig.value = true
  try {
    const res = await axios.put('/api/channels/import-config', importPayload(), {
      headers: authHeaders()
    })
    importForm.value = {
      url: res.data?.url || '',
      token: res.data?.token || '',
      userId: res.data?.userId || '1'
    }
    ElMessage.success('导入配置已保存')
    return true
  } catch (err) {
    const status = err.response?.status
    ElMessage.error(err.response?.data?.error || (status ? `导入配置保存失败（HTTP ${status}）` : '导入配置保存失败'))
    return false
  } finally {
    savingImportConfig.value = false
  }
}

const importChannels = async () => {
  if (!validateImportForm()) return

  try {
    await ElMessageBox.confirm(
      '导入会覆盖当前站点列表，地址重复时优先保留启用渠道，同状态保留最后一条。确认继续吗?',
      '确认导入',
      { type: 'warning' }
    )
  } catch {
    return
  }

  importing.value = true
  try {
    const res = await axios.post('/api/channels/import', importPayload(), {
      headers: authHeaders()
    })
    await fetchSites()
    importDialogVisible.value = false
    const sourceCount = res.data?.source_count || 0
    const importedCount = res.data?.imported_count || 0
    const duplicateCount = res.data?.duplicate_url_count || 0
    const invalidCount = res.data?.invalid_url_count || 0
    const invalidText = invalidCount > 0 ? `，无效地址 ${invalidCount} 条` : ''
    ElMessage.success(`源数据 ${sourceCount} 条，按渠道URL去重后导入 ${importedCount} 个，重复地址 ${duplicateCount} 条${invalidText}`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '导入渠道失败')
  } finally {
    importing.value = false
  }
}

const exportChannels = () => {
  if (!sites.value.length) {
    ElMessage.warning('暂无渠道可导出')
    return
  }

  const rows = [
    ['渠道ID', '状态码', '状态', '渠道名称', '渠道URL', '适配', 'Token', 'User ID', '余额USD'],
    ...sites.value.map(site => [
      site.channelId || '',
      site.status || 0,
      channelStatusLabel(site.status),
      site.name || '',
      site.url || '',
      siteAdapterLabel(site.adapter),
      site.token || '',
      site.userId || '',
      site.balance !== undefined ? site.balance.toFixed(4) : ''
    ])
  ]

  const csv = `\uFEFF${rows.map(row => row.map(csvCell).join(',')).join('\r\n')}`
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const downloadUrl = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = downloadUrl
  link.download = `new-api-balance-channels-${exportTimestamp()}.csv`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(downloadUrl)
}

const csvCell = (value) => {
  const text = String(value ?? '')
  return `"${text.replace(/"/g, '""')}"`
}

const exportTimestamp = () => {
  const date = new Date()
  const pad = (value) => String(value).padStart(2, '0')
  return [
    date.getFullYear(),
    pad(date.getMonth() + 1),
    pad(date.getDate())
  ].join('') + '-' + [
    pad(date.getHours()),
    pad(date.getMinutes()),
    pad(date.getSeconds())
  ].join('')
}

const refreshAll = async () => {
  refreshing.value = true
  const promises = sites.value.map(async (site) => {
    await fetchSiteBalance(site)
  })
  await Promise.all(promises)
  refreshing.value = false
}

const refreshSite = async (site) => {
  const key = siteRefreshKey(site)
  setSiteRefreshing(key, true)
  try {
    await fetchSiteBalance(site)
    if (!site.error) {
      ElMessage.success('余额已刷新')
    } else {
      ElMessage.error(site.error)
    }
  } finally {
    setSiteRefreshing(key, false)
  }
}

const fetchSiteBalance = async (site) => {
  try {
    if (!site.id) {
      site.balance = undefined
      site.error = '请先保存配置后再刷新余额'
      return
    }
    site.error = ''
    const res = await axios.post('/api/balance/query', balanceQueryPayload(site), {
      headers: authHeaders()
    })
    const data = res.data
    if (data?.ok && data.balance !== undefined) {
      site.balance = Number(data.balance)
    } else {
      site.balance = undefined
      site.error = data?.error || '数据格式错误'
    }
  } catch (err) {
    site.balance = undefined
    site.error = err.response?.data?.error || '请求失败'
  }
}

const balanceQueryPayload = (site) => ({
  id: site.id || ''
})

const siteRefreshKey = (site) => `${site.id || ''}|${site.channelId || ''}|${site.url || ''}`

const isRefreshingSite = (site) => refreshingSiteKeys.value.has(siteRefreshKey(site))

const setSiteRefreshing = (key, value) => {
  const next = new Set(refreshingSiteKeys.value)
  if (value) {
    next.add(key)
  } else {
    next.delete(key)
  }
  refreshingSiteKeys.value = next
}

const openNotificationDialog = async () => {
  notificationDialogVisible.value = true
  notificationLoading.value = true
  await fetchNotificationConfig()
  notificationLoading.value = false
}

const notificationPayload = () => ({
  enabled: notificationConfig.value.enabled,
  notification_type: notificationConfig.value.notification_type,
  webhook_url: notificationConfig.value.webhook_url,
  sign_key: notificationConfig.value.sign_key,
  wework_webhook_url: notificationConfig.value.wework_webhook_url,
  interval_minutes: notificationConfig.value.interval_minutes,
  balance_threshold: Number(notificationConfig.value.balance_threshold || 0)
})

const saveNotificationConfig = async (silent = false) => {
  if (typeof silent !== 'boolean') {
    silent = false
  }
  savingNotification.value = true
  try {
    const res = await axios.put('/api/notification', notificationPayload(), {
      headers: authHeaders()
    })
    notificationConfig.value = {
      ...defaultNotificationConfig(),
      ...res.data
    }
    if (!silent) {
      ElMessage.success('通知配置已保存')
    }
    return true
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '通知配置保存失败')
    return false
  } finally {
    savingNotification.value = false
  }
}

const testNotification = async () => {
  testingNotification.value = true
  try {
    const saved = await saveNotificationConfig(true)
    if (!saved) return

    const res = await axios.post('/api/notification/test', {}, {
      headers: authHeaders()
    })
    if (res.data?.success) {
      ElMessage.success(res.data.message || '通知测试成功')
    } else {
      ElMessage.error(res.data?.message || '通知测试失败')
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.message || '通知测试失败')
  } finally {
    testingNotification.value = false
  }
}

const sendBalanceNotification = async () => {
  notifying.value = true
  try {
    const res = await axios.post('/api/notification/send-now', {}, {
      headers: authHeaders()
    })
    ElMessage.success(res.data?.message || '余额通知已发送')
    await fetchNotificationConfig()
  } catch (err) {
    ElMessage.error(err.response?.data?.message || '余额通知发送失败')
    await fetchNotificationConfig()
  } finally {
    notifying.value = false
  }
}

const normalizeSiteUrl = (value) => {
  const trimmed = (value || '').trim().replace(/\/+$/, '')
  if (!trimmed) return ''
  if (/^https?:\/\//i.test(trimmed)) return trimmed
  return `https://${trimmed}`
}

const siteBalanceEndpoint = (site) => {
  const normalized = normalizeSiteUrl(site.url)
  if (normalized.replace(/\/+$/, '').endsWith('/api/user/self')) {
    return normalized
  }
  return `${normalized}/api/user/self`
}

const siteAdapter = (site) => (site.adapter || '').trim().toLowerCase()

const siteAdapterLabel = (adapter) => {
  const value = (adapter || '').trim().toLowerCase()
  if (value === 'qingshan') return '青山'
  if (value === 'ephone') return 'ePhone'
  return '默认'
}

const quotaToUsd = (quota) => quota / 500000

const channelStatusLabel = (status) => {
  const value = Number(status || 0)
  if (value === 1) return '启用'
  if (value === 2) return '禁用'
  if (value === 0) return '未知'
  return `状态 ${value}`
}

const channelStatusType = (status) => {
  const value = Number(status || 0)
  if (value === 1) return 'success'
  if (value === 2) return 'danger'
  if (value === 0) return 'info'
  return 'warning'
}

const formatDateTime = (value) => {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

const openAddSite = () => {
  isEdit.value = false
  currentSite.value = { channelId: 0, status: 0, name: '', url: '', adapter: '', token: '', userId: '' }
  dialogVisible.value = true
}

const siteIndexOf = (site) => sites.value.indexOf(site)

const editSite = (site) => {
  const index = siteIndexOf(site)
  if (index < 0) return
  isEdit.value = true
  editIndex.value = index
  currentSite.value = { ...sites.value[index] }
  dialogVisible.value = true
}

const saveSite = () => {
  currentSite.value.url = normalizeSiteUrl(currentSite.value.url)
  if (isEdit.value) {
    sites.value[editIndex.value] = { ...sites.value[editIndex.value], ...currentSite.value }
  } else {
    sites.value.push({ ...currentSite.value })
  }
  dialogVisible.value = false
  syncToServer()
}

const deleteSite = (site) => {
  const index = siteIndexOf(site)
  if (index < 0) return
  ElMessageBox.confirm('确认删除该站点吗?', '提示', { type: 'warning' }).then(() => {
    sites.value.splice(index, 1)
    syncToServer()
  }).catch(() => {})
}

const logout = () => {
  localStorage.removeItem('token')
  router.push('/login')
}

onMounted(() => {
  fetchSites()
  fetchNotificationConfig()
})
</script>

<style scoped>
.balance-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.notification-alert {
  margin-bottom: 16px;
}

.table-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.filter-summary {
  color: #606266;
  font-size: 14px;
}

.import-alert {
  margin-bottom: 18px;
}

.form-suffix {
  margin-left: 8px;
  color: #606266;
}

@media (max-width: 768px) {
  .header {
    align-items: flex-start;
    flex-direction: column;
  }

  .actions {
    justify-content: flex-start;
  }
}
</style>
