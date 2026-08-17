<template>
  <div class="balance-container">
    <div class="header">
      <h2>渠道余额</h2>
      <div class="actions">
        <el-button type="primary" @click="refreshAll" :loading="refreshing">刷新所有余额</el-button>
        <el-button type="warning" plain :icon="Star" @click="queryKeyChannelsBalance" :loading="queryingKeyBalance">查询重点渠道余额</el-button>
        <el-button type="info" @click="openImportDialog">导入渠道</el-button>
        <el-button @click="exportChannels">导出渠道</el-button>
        <el-button type="warning" @click="sendBalanceNotification" :loading="notifying">立即推送</el-button>
        <el-button @click="openNotificationDialog">通知设置</el-button>
        <el-button type="success" @click="openAddSite">添加站点</el-button>
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
      <el-checkbox v-model="keyOnlyFilter">只看重点渠道</el-checkbox>
      <span class="filter-summary">显示 {{ filteredSites.length }} / {{ sites.length }} 个渠道</span>
    </div>

    <el-table :data="filteredSites" style="width: 100%" border v-loading="loading">
      <el-table-column prop="channelId" label="渠道ID" width="90" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="channelStatusType(row.status)">{{ channelStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="重点" width="70" align="center">
        <template #default="{ row }">
          <el-tooltip :content="row.isKey ? '取消重点标记' : '标记为重点渠道'" placement="top">
            <el-icon
              class="key-star"
              :class="{ 'key-star--active': row.isKey }"
              @click="toggleKeySite(row)"
            >
              <component :is="row.isKey ? StarFilled : Star" />
            </el-icon>
          </el-tooltip>
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
            <el-option label="grisa" value="grisa" />
          </el-select>
        </el-form-item>
        <el-form-item label="Token">
          <el-input v-model="currentSite.token" placeholder="Bearer sk-xxxx" />
        </el-form-item>
        <el-form-item label="User ID">
          <el-input v-model="currentSite.userId" placeholder="可选" />
        </el-form-item>
        <el-form-item label="管理员账号">
          <el-input v-model="currentSite.adminAccount" placeholder="可选，用于记录上游站点的登录账号" />
        </el-form-item>
        <el-form-item label="管理员密码">
          <el-input v-model="currentSite.adminPassword" type="password" show-password placeholder="可选，用于记录上游站点的登录密码" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="currentSite.remark" type="textarea" :rows="2" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" @click="saveSite">保存</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog title="从上游导入渠道" v-model="importDialogVisible" width="520px">
      <el-alert
        class="import-alert"
        type="warning"
        :closable="false"
        title="导入会覆盖当前站点列表；地址重复时优先保留启用渠道，同状态保留上游返回中的最后一条。已有相同地址的 Token 和 User ID 会自动保留。"
      />
      <el-form label-width="100px" v-loading="importConfigLoading">
        <el-form-item label="上游站点">
          <el-select v-model="importSiteId" placeholder="请选择上游站点" style="width: 100%">
            <el-option v-for="s in importSiteList" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button text type="primary" @click="$router.push('/upstream-sites')">管理站点</el-button>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="importDialogVisible = false">取消</el-button>
          <el-button type="primary" @click="importChannels" :loading="importing" :disabled="!importSiteId">开始导入</el-button>
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
        <el-form-item label="推送计划">
          <div class="schedule-list">
            <div
              v-for="(schedule, index) in notificationConfig.schedules"
              :key="index"
              class="schedule-row"
            >
              <el-time-picker
                v-model="schedule.start_time"
                format="HH:mm"
                value-format="HH:mm"
                placeholder="开始"
                style="width: 120px"
              />
              <span class="schedule-separator">至</span>
              <el-time-picker
                v-model="schedule.end_time"
                format="HH:mm"
                value-format="HH:mm"
                placeholder="结束"
                style="width: 120px"
              />
              <el-input-number
                v-model="schedule.interval_minutes"
                :min="1"
                :max="1440"
                :step="10"
                controls-position="right"
                style="width: 130px"
              />
              <span class="form-suffix">分钟</span>
              <el-button type="danger" link @click="removeNotificationSchedule(index)">删除</el-button>
            </div>
            <el-button @click="addNotificationSchedule">添加计划</el-button>
          </div>
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
        <el-form-item label="红色预警值">
          <el-input-number
            v-model="notificationConfig.red_balance_threshold"
            :min="0.01"
            :precision="2"
            :step="50"
          />
          <span class="form-suffix">USD 及以下标红</span>
        </el-form-item>
        <el-form-item label="黄色预警值">
          <el-input-number
            v-model="notificationConfig.yellow_balance_threshold"
            :min="0.01"
            :precision="2"
            :step="50"
          />
          <span class="form-suffix">USD 及以下标黄，高于此值标绿</span>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Star, StarFilled } from '@element-plus/icons-vue'
import axios from 'axios'

const sites = ref([])
const loading = ref(false)
const refreshing = ref(false)
const refreshingSiteKeys = ref(new Set())
const notifying = ref(false)
const importing = ref(false)
const importConfigLoading = ref(false)
const statusFilter = ref('all')
const keyOnlyFilter = ref(false)
const queryingKeyBalance = ref(false)
const defaultRedBalanceThreshold = 100
const defaultYellowBalanceThreshold = 500

const dialogVisible = ref(false)
const isEdit = ref(false)
const editIndex = ref(-1)
const currentSite = ref({ channelId: 0, status: 0, name: '', url: '', adapter: '', token: '', userId: '', adminAccount: '', adminPassword: '', remark: '', isKey: false })

const importDialogVisible = ref(false)
const importSiteId = ref('')
const importSiteList = ref([])

const defaultNotificationConfig = () => ({
  enabled: false,
  notification_type: 'feishu',
  webhook_url: '',
  sign_key: '',
  wework_webhook_url: '',
  interval_minutes: 60,
  schedules: [],
  balance_threshold: 0,
  red_balance_threshold: defaultRedBalanceThreshold,
  yellow_balance_threshold: defaultYellowBalanceThreshold,
  last_attempt_at: '',
  last_sent_at: '',
  last_error: ''
})

const normalizeNotificationConfigData = (data = {}) => ({
  ...defaultNotificationConfig(),
  ...data,
  red_balance_threshold: Number(data?.red_balance_threshold || defaultRedBalanceThreshold),
  yellow_balance_threshold: Number(data?.yellow_balance_threshold || defaultYellowBalanceThreshold),
  schedules: (data?.schedules || []).map(schedule => ({
    start_time: schedule.start_time || '',
    end_time: schedule.end_time || '',
    interval_minutes: Number(schedule.interval_minutes || 60)
  }))
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
  const redThreshold = Number(notificationConfig.value.red_balance_threshold || defaultRedBalanceThreshold)
  const yellowThreshold = Number(notificationConfig.value.yellow_balance_threshold || defaultYellowBalanceThreshold)
  const thresholdText = threshold > 0 ? `，仅推送低于 ${threshold.toFixed(2)} USD 的渠道` : '，推送全部渠道'
  const warningText = `，红色 <= ${redThreshold.toFixed(2)} USD，黄色 <= ${yellowThreshold.toFixed(2)} USD`
  const scheduleCount = notificationConfig.value.schedules?.length || 0
  const scheduleText = scheduleCount > 0
    ? `${scheduleCount} 个推送计划`
    : `每 ${notificationConfig.value.interval_minutes} 分钟`
  return `余额通知已启用，${scheduleText}推送至 ${notificationTypeLabel.value}${thresholdText}${warningText}`
})

const filteredSites = computed(() => {
  let result = sites.value
  if (statusFilter.value !== 'all') {
    const status = Number(statusFilter.value)
    result = result.filter(site => Number(site.status || 0) === status)
  }
  if (keyOnlyFilter.value) {
    result = result.filter(site => site.isKey)
  }
  return result
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
    notificationConfig.value = normalizeNotificationConfigData(res.data)
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
      userId: s.userId,
      adminAccount: s.adminAccount || '',
      adminPassword: s.adminPassword || '',
      remark: s.remark || '',
      isKey: !!s.isKey
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
  importSiteId.value = ''
  try {
    const res = await axios.get('/api/upstream-sites', { headers: authHeaders() })
    importSiteList.value = res.data || []
    if (importSiteList.value.length === 1) {
      importSiteId.value = importSiteList.value[0].id
    }
  } catch {
    ElMessage.error('加载站点列表失败')
  } finally {
    importConfigLoading.value = false
  }
}

const importChannels = async () => {
  if (!importSiteId.value) {
    ElMessage.warning('请选择上游站点')
    return
  }

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
    const res = await axios.post('/api/channels/import', {
      upstreamSiteId: importSiteId.value
    }, {
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
    ['渠道ID', '状态码', '状态', '重点渠道', '渠道名称', '渠道URL', '适配', 'Token', 'User ID', '余额USD'],
    ...sites.value.map(site => [
      site.channelId || '',
      site.status || 0,
      channelStatusLabel(site.status),
      site.isKey ? '是' : '否',
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

const toggleKeySite = (site) => {
  site.isKey = !site.isKey
  syncToServer()
}

const queryKeyChannelsBalance = async () => {
  const keySites = sites.value.filter(site => site.isKey)
  if (!keySites.length) {
    ElMessage.warning('暂无标记的重点渠道')
    return
  }
  keyOnlyFilter.value = true
  queryingKeyBalance.value = true
  try {
    await Promise.all(keySites.map(site => fetchSiteBalance(site)))
    ElMessage.success(`已刷新 ${keySites.length} 个重点渠道余额`)
  } finally {
    queryingKeyBalance.value = false
  }
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

const addNotificationSchedule = () => {
  const schedules = notificationConfig.value.schedules || []
  const lastSchedule = schedules[schedules.length - 1]
  const startTime = lastSchedule?.end_time || '08:00'
  notificationConfig.value.schedules = [
    ...schedules,
    {
      start_time: startTime,
      end_time: addMinutesToTime(startTime, 60),
      interval_minutes: Number(notificationConfig.value.interval_minutes || 60)
    }
  ]
}

const removeNotificationSchedule = (index) => {
  notificationConfig.value.schedules = (notificationConfig.value.schedules || []).filter((_, i) => i !== index)
}

const addMinutesToTime = (timeText, minutesToAdd) => {
  const match = /^(\d{1,2}):(\d{2})$/.exec(timeText || '')
  if (!match) return '09:00'
  const total = ((Number(match[1]) * 60 + Number(match[2]) + minutesToAdd) % (24 * 60) + (24 * 60)) % (24 * 60)
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

const notificationPayload = () => ({
  enabled: notificationConfig.value.enabled,
  notification_type: notificationConfig.value.notification_type,
  webhook_url: notificationConfig.value.webhook_url,
  sign_key: notificationConfig.value.sign_key,
  wework_webhook_url: notificationConfig.value.wework_webhook_url,
  interval_minutes: notificationConfig.value.interval_minutes,
  schedules: notificationSchedulesPayload(),
  balance_threshold: Number(notificationConfig.value.balance_threshold || 0),
  red_balance_threshold: Number(notificationConfig.value.red_balance_threshold || defaultRedBalanceThreshold),
  yellow_balance_threshold: Number(notificationConfig.value.yellow_balance_threshold || defaultYellowBalanceThreshold)
})

const validateNotificationThresholds = () => {
  const redThreshold = Number(notificationConfig.value.red_balance_threshold)
  const yellowThreshold = Number(notificationConfig.value.yellow_balance_threshold)

  if (!Number.isFinite(redThreshold) || redThreshold <= 0) {
    ElMessage.error('红色预警值必须大于 0')
    return false
  }
  if (!Number.isFinite(yellowThreshold) || yellowThreshold <= 0) {
    ElMessage.error('黄色预警值必须大于 0')
    return false
  }
  if (redThreshold > yellowThreshold) {
    ElMessage.error('红色预警值不能大于黄色预警值')
    return false
  }
  return true
}

const notificationSchedulesPayload = () => (
  (notificationConfig.value.schedules || []).map(schedule => ({
    start_time: schedule.start_time || '',
    end_time: schedule.end_time || '',
    interval_minutes: Number(schedule.interval_minutes || 0)
  }))
)

const saveNotificationConfig = async (silent = false) => {
  if (typeof silent !== 'boolean') {
    silent = false
  }
  if (!validateNotificationThresholds()) return false

  savingNotification.value = true
  try {
    const res = await axios.put('/api/notification', notificationPayload(), {
      headers: authHeaders()
    })
    notificationConfig.value = normalizeNotificationConfigData(res.data)
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
  if (value === 'grisa') return 'grisa'
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
  currentSite.value = { channelId: 0, status: 0, name: '', url: '', adapter: '', token: '', userId: '', isKey: false }
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
  window.location.href = '/login'
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

.key-star {
  cursor: pointer;
  font-size: 18px;
  color: #c0c4cc;
}

.key-star:hover {
  color: #e6a23c;
}

.key-star--active {
  color: #f7ba2a;
}

.import-alert {
  margin-bottom: 18px;
}

.form-suffix {
  margin-left: 8px;
  color: #606266;
}

.schedule-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.schedule-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.schedule-separator {
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
