<template>
  <div class="dashboard-container">
    <div class="header">
      <h2>管理面板</h2>
      <div class="actions">
        <span class="label">选择日期:</span>
        <el-date-picker
          v-model="selectedDate"
          type="date"
          placeholder="选择日期"
          value-format="YYYY-MM-DD"
          style="width: 160px"
        />
        <el-select v-model="selectedComputeSiteId" placeholder="选择站点" style="width: 200px">
          <el-option label="全部站点" value="" />
          <el-option v-for="s in availableSites" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
        <el-button type="primary" :icon="Refresh" @click="loadStats" :loading="loadingStats">查询</el-button>
        <el-button type="success" :icon="VideoPlay" @click="computeToday" :loading="computing">
          {{ computing ? '计算中...' : '计算当日数据' }}
        </el-button>
        <el-button type="warning" @click="openNotifyDialog">战绩推送设置</el-button>
        <el-button type="info" @click="sendNow" :loading="pushingNow">立即推送</el-button>
      </div>
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 16px" />

    <!-- Step progress display -->
    <div v-if="computing || computeSteps.length > 0" class="step-progress-section" style="margin-bottom: 16px">
      <div v-for="task in computeSteps" :key="task.siteName" class="step-progress-card">
        <div class="step-progress-title">{{ task.siteName }}</div>
        <div class="step-progress-list">
          <div v-for="step in getStepList(task)" :key="step.key" class="step-item">
            <span :class="['step-icon', step.status]">
              {{ step.status === 'completed' ? '✅' : step.status === 'running' ? '⏳' : step.status === 'failed' ? '❌' : '⏸' }}
            </span>
            <span class="step-name">{{ step.label }}</span>
            <span v-if="step.error" class="step-error">{{ step.error }}</span>
          </div>
        </div>
      </div>
      <div v-if="hasFailedSteps" style="margin-top: 8px">
        <el-button type="warning" size="small" @click="retryFailed" :loading="computing">
          重试失败步骤
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="computing"
      type="info"
      :closable="false"
      title="正在从上游站点拉取日志并计算统计数据，请耐心等待..."
      style="margin-bottom: 16px"
    />

    <!-- Section 1: Per-site consumption cards -->
    <div class="consumption-section">
      <div class="site-cards">
        <div v-for="sc in siteConsumptions" :key="sc.siteId" class="site-card">
          <div class="site-card-header">{{ sc.siteName }}</div>
          <div class="site-card-body">
            <div class="consumption-value">{{ formatQuota(sc.todayQuota) }}</div>
            <div class="consumption-label">今日消费</div>
            <div class="consumption-compare">
              <span class="yesterday-value">昨日: {{ formatQuota(sc.yesterdayQuota) }}</span>
              <span :class="['change-rate', sc.changeRate > 0 ? 'up' : sc.changeRate < 0 ? 'down' : 'flat']">
                {{ sc.changeRate > 0 ? '+' : '' }}{{ sc.changeRate.toFixed(2) }}%
              </span>
            </div>
          </div>
        </div>
        <div v-if="siteConsumptions.length > 1" class="site-card total-card">
          <div class="site-card-header">总计</div>
          <div class="site-card-body">
            <div class="consumption-value total">{{ formatQuota(totalTodayQuota) }}</div>
            <div class="consumption-label">今日消费</div>
            <div class="consumption-compare">
              <span class="yesterday-value">昨日: {{ formatQuota(totalYesterdayQuota) }}</span>
              <span :class="['change-rate', totalChangeRate > 0 ? 'up' : totalChangeRate < 0 ? 'down' : 'flat']">
                {{ totalChangeRate > 0 ? '+' : '' }}{{ totalChangeRate.toFixed(2) }}%
              </span>
            </div>
          </div>
        </div>
      </div>
      <el-empty v-if="!loadingStats && siteConsumptions.length === 0 && !errorMsg" description="暂无统计数据，请先点击「计算当日数据」" />
    </div>

    <!-- Section 2: Rankings -->
    <div v-if="rankingData" class="ranking-section">
      <div class="ranking-header">
        <el-select v-model="selectedRankingSiteId" placeholder="选择站点" style="width: 200px" @change="updateRanking">
          <el-option label="全部站点 (汇总)" value="__all__" />
          <el-option v-for="s in availableSites" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
      </div>
      <div class="ranking-tables">
        <div class="ranking-card">
          <div class="ranking-title">模型排行</div>
          <div class="ranking-list">
            <div v-for="(item, idx) in rankingData.modelRanking" :key="'m'+idx" class="ranking-item">
              <span :class="['rank-badge', rankClass(idx)]">{{ idx + 1 }}</span>
              <span class="rank-name">{{ item.name }}</span>
              <span class="rank-value">{{ formatQuota(item.quota) }}</span>
            </div>
            <div v-if="!rankingData.modelRanking?.length" class="ranking-empty">暂无数据</div>
          </div>
        </div>
        <div class="ranking-card">
          <div class="ranking-title">渠道排行</div>
          <div class="ranking-list">
            <div v-for="(item, idx) in rankingData.channelRanking" :key="'c'+idx" class="ranking-item">
              <span :class="['rank-badge', rankClass(idx)]">{{ idx + 1 }}</span>
              <span class="rank-name">{{ item.name }}</span>
              <span class="rank-value">{{ formatQuota(item.quota) }}</span>
            </div>
            <div v-if="!rankingData.channelRanking?.length" class="ranking-empty">暂无数据</div>
          </div>
        </div>
        <div class="ranking-card">
          <div class="ranking-title">用户排行</div>
          <div class="ranking-list">
            <div v-for="(item, idx) in rankingData.userRanking" :key="'u'+idx" class="ranking-item">
              <span :class="['rank-badge', rankClass(idx)]">{{ idx + 1 }}</span>
              <span class="rank-name">{{ item.name }}</span>
              <span class="rank-value">{{ formatQuota(item.quota) }}</span>
            </div>
            <div v-if="!rankingData.userRanking?.length" class="ranking-empty">暂无数据</div>
          </div>
        </div>
        <div class="ranking-card">
          <div class="ranking-title">错误模型排行</div>
          <div class="ranking-list">
            <div v-for="(item, idx) in rankingData.errorModelRanking" :key="'e'+idx" class="ranking-item">
              <span :class="['rank-badge', rankClass(idx)]">{{ idx + 1 }}</span>
              <span class="rank-name">{{ item.name }}</span>
              <span class="rank-value error-count">{{ item.count }} 次</span>
            </div>
            <div v-if="!rankingData.errorModelRanking?.length" class="ranking-empty">暂无数据</div>
          </div>
        </div>
      </div>
    </div>

    <!-- Section 3: Start date config + batch compute -->
    <el-divider />
    <div class="config-section">
      <h3>批量计算配置</h3>
      <div class="config-form">
        <div class="config-row">
          <span class="config-label">开始计算日期:</span>
          <el-date-picker
            v-model="startDate"
            type="date"
            placeholder="选择开始日期"
            value-format="YYYY-MM-DD"
            style="width: 160px"
          />
          <span class="config-label">请求并发数:</span>
          <el-input-number v-model="concurrency" :min="1" :max="50" :step="1" style="width: 120px" />
          <el-select v-model="batchSiteId" placeholder="选择站点" style="width: 200px">
            <el-option label="全部站点" value="" />
            <el-option v-for="s in availableSites" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
          <el-button type="primary" plain @click="saveConfig" :loading="savingConfig">保存配置</el-button>
          <el-button type="warning" @click="batchCompute" :loading="batchComputing">
            {{ batchComputing ? '批量计算中...' : '批量计算' }}
          </el-button>
          <span class="config-hint">从开始日期到今天，逐日计算选定站点的消费数据并存储到数据库</span>
        </div>
      </div>

      <el-alert
        v-if="batchComputing"
        type="info"
        :closable="false"
        title="批量计算进行中，大量数据拉取可能需要较长时间，请勿关闭页面..."
        style="margin-top: 12px"
      />
      <el-alert
        v-if="batchResult"
        :type="batchResult.errors?.length ? 'warning' : 'success'"
        :closable="true"
        style="margin-top: 12px"
      >
        <template #title>
          批量计算完成: 成功 {{ batchResult.computed }} 条
          <span v-if="batchResult.errors?.length">，{{ batchResult.errors.length }} 个错误</span>
        </template>
      </el-alert>
    </div>

    <!-- Dashboard Notification Dialog -->
    <el-dialog title="战绩推送设置" v-model="notifyDialogVisible" width="620px">
      <el-form :model="notifyConfig" label-width="140px">
        <el-form-item label="启用推送">
          <el-switch v-model="notifyConfig.enabled" />
        </el-form-item>
        <el-form-item label="通知方式">
          <el-radio-group v-model="notifyConfig.notification_type">
            <el-radio value="feishu">飞书机器人</el-radio>
            <el-radio value="wework">企业微信机器人</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="notifyConfig.notification_type === 'feishu'" label="飞书 Webhook URL">
          <el-input v-model="notifyConfig.webhook_url" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" clearable />
        </el-form-item>
        <el-form-item v-if="notifyConfig.notification_type === 'feishu'" label="签名校验密钥">
          <el-input v-model="notifyConfig.sign_key" placeholder="可选，飞书机器人安全设置中的签名校验" clearable />
        </el-form-item>
        <el-form-item v-if="notifyConfig.notification_type === 'wework'" label="企微 Webhook URL">
          <el-input v-model="notifyConfig.wework_webhook_url" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" clearable />
        </el-form-item>
        <el-form-item label="每日推送时间">
          <el-time-picker v-model="notifyConfig.push_time" format="HH:mm" value-format="HH:mm" placeholder="08:00" />
          <span style="margin-left: 8px; font-size: 12px; color: #909399;">每天在此时间推送前一天的战绩</span>
        </el-form-item>
        <el-form-item label="排行显示数量">
          <el-input-number v-model="notifyConfig.top_n" :min="3" :max="20" />
        </el-form-item>
        <el-form-item label="自动计算">
          <el-switch v-model="notifyConfig.auto_compute" />
          <span style="margin-left: 8px; font-size: 12px; color: #909399;">推送前自动计算前一天数据（如未计算过）</span>
        </el-form-item>
        <el-divider v-if="notifyConfig.last_attempt_at || notifyConfig.last_error" />
        <el-form-item v-if="notifyConfig.last_attempt_at" label="上次尝试">
          <span style="font-size: 13px; color: #606266;">{{ formatTime(notifyConfig.last_attempt_at) }}</span>
        </el-form-item>
        <el-form-item v-if="notifyConfig.last_sent_at" label="上次成功">
          <span style="font-size: 13px; color: #67c23a;">{{ formatTime(notifyConfig.last_sent_at) }}</span>
          <span v-if="notifyConfig.last_sent_date" style="margin-left: 8px; font-size: 12px; color: #909399;">（{{ notifyConfig.last_sent_date }}）</span>
        </el-form-item>
        <el-form-item v-if="notifyConfig.last_error" label="上次错误">
          <span style="font-size: 13px; color: #f56c6c;">{{ notifyConfig.last_error }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="notifyDialogVisible = false">取消</el-button>
        <el-button @click="testNotify" :loading="testingNotify">测试通知</el-button>
        <el-button type="primary" @click="saveNotifyConfig" :loading="savingNotify">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { Refresh, VideoPlay } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const todayLocal = () => {
  const now = new Date()
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}
const selectedDate = ref(todayLocal())
const selectedComputeSiteId = ref('')
const loadingStats = ref(false)
const computing = ref(false)
const errorMsg = ref('')

const todayStatsData = ref([])
const yesterdayStatsData = ref([])
const availableSites = ref([])
const selectedRankingSiteId = ref('__all__')

const startDate = ref('')
const concurrency = ref(5)
const batchSiteId = ref('')
const savingConfig = ref(false)
const batchComputing = ref(false)
const batchResult = ref(null)

const computeSteps = ref([])
let pollTimer = null

const stepLabels = {
  paginationStatus: '分页数据采集',
  modelRankingStatus: '模型排行精排',
  channelRankingStatus: '渠道排行精排',
  userRankingStatus: '用户排行精排',
  errorModelRankingStatus: '错误模型排行'
}
const stepErrorFields = {
  paginationStatus: 'paginationError',
  modelRankingStatus: 'modelRankingError',
  channelRankingStatus: 'channelRankingError',
  userRankingStatus: 'userRankingError',
  errorModelRankingStatus: 'errorModelRankingError'
}

const getStepList = (task) => {
  return Object.entries(stepLabels).map(([key, label]) => ({
    key,
    label,
    status: task[key] || 'pending',
    error: task[stepErrorFields[key]] || ''
  }))
}

const hasFailedSteps = computed(() => {
  return computeSteps.value.some(task =>
    Object.keys(stepLabels).some(key => task[key] === 'failed')
  )
})

const pollComputeStatus = async () => {
  try {
    const params = { date: selectedDate.value }
    if (selectedComputeSiteId.value) {
      params.siteId = selectedComputeSiteId.value
    }
    const res = await axios.get('/api/dashboard/compute-status', {
      params,
      headers: authHeaders()
    })
    computeSteps.value = res.data || []
  } catch { /* ignore */ }
}

const startPolling = () => {
  stopPolling()
  pollComputeStatus()
  pollTimer = setInterval(pollComputeStatus, 3000)
}

const stopPolling = () => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

const retryFailed = async () => {
  computing.value = true
  errorMsg.value = ''
  startPolling()
  try {
    const body = { date: selectedDate.value }
    if (selectedComputeSiteId.value) {
      body.siteId = selectedComputeSiteId.value
    }
    const res = await axios.post('/api/dashboard/compute',
      body,
      { headers: authHeaders(), timeout: 600000 }
    )
    ElMessage.success(`计算完成: ${res.data.computed} 条记录`)
    if (res.data.errors?.length) {
      ElMessage.warning(`${res.data.errors.length} 个错误`)
    }
    await pollComputeStatus()
    await loadStats()
  } catch (e) {
    errorMsg.value = e.response?.data?.error || '重试失败'
  } finally {
    computing.value = false
    stopPolling()
  }
}

const notifyDialogVisible = ref(false)
const notifyConfig = ref({
  enabled: false,
  notification_type: 'feishu',
  webhook_url: '',
  sign_key: '',
  wework_webhook_url: '',
  push_time: '08:00',
  top_n: 10,
  auto_compute: false,
  last_attempt_at: null,
  last_sent_at: null,
  last_sent_date: '',
  last_error: ''
})
const savingNotify = ref(false)
const testingNotify = ref(false)
const pushingNow = ref(false)

const formatQuota = (quota) => {
  if (quota == null || quota === undefined) return '¥0.00'
  const val = quota / 500000
  if (val >= 1) return '¥' + val.toFixed(2)
  if (val > 0) return '¥' + val.toFixed(4)
  return '¥0.00'
}

const siteConsumptions = computed(() => {
  if (!todayStatsData.value.length) return []

  return todayStatsData.value.map(ts => {
    const ys = yesterdayStatsData.value.find(y => y.upstreamSiteId === ts.upstreamSiteId)
    const yesterdayQuota = ys ? ys.totalQuota : 0
    let changeRate = 0
    if (yesterdayQuota > 0) {
      changeRate = ((ts.totalQuota - yesterdayQuota) / yesterdayQuota) * 100
    } else if (ts.totalQuota > 0) {
      changeRate = 100
    }
    return {
      siteId: ts.upstreamSiteId,
      siteName: ts.siteName,
      todayQuota: ts.totalQuota,
      yesterdayQuota,
      changeRate
    }
  })
})

const totalTodayQuota = computed(() => {
  return todayStatsData.value.reduce((sum, s) => sum + (s.totalQuota || 0), 0)
})

const totalYesterdayQuota = computed(() => {
  return yesterdayStatsData.value.reduce((sum, s) => sum + (s.totalQuota || 0), 0)
})

const totalChangeRate = computed(() => {
  if (totalYesterdayQuota.value > 0) {
    return ((totalTodayQuota.value - totalYesterdayQuota.value) / totalYesterdayQuota.value) * 100
  }
  return totalTodayQuota.value > 0 ? 100 : 0
})

const rankingData = computed(() => {
  if (!todayStatsData.value.length) return null

  if (selectedRankingSiteId.value !== '__all__') {
    const site = todayStatsData.value.find(s => s.upstreamSiteId === selectedRankingSiteId.value)
    if (!site) return { modelRanking: [], channelRanking: [], userRanking: [], errorModelRanking: [] }
    return {
      modelRanking: site.modelRanking || [],
      channelRanking: site.channelRanking || [],
      userRanking: site.userRanking || [],
      errorModelRanking: site.errorModelRanking || []
    }
  }

  const modelMap = {}
  const channelMap = {}
  const userMap = {}
  const errorModelMap = {}

  for (const site of todayStatsData.value) {
    for (const item of (site.modelRanking || [])) {
      modelMap[item.name] = (modelMap[item.name] || 0) + item.quota
    }
    for (const item of (site.channelRanking || [])) {
      channelMap[item.name] = (channelMap[item.name] || 0) + item.quota
    }
    for (const item of (site.userRanking || [])) {
      userMap[item.name] = (userMap[item.name] || 0) + item.quota
    }
    for (const item of (site.errorModelRanking || [])) {
      errorModelMap[item.name] = (errorModelMap[item.name] || 0) + item.count
    }
  }

  const toRankList = (map, key) => Object.entries(map)
    .map(([name, val]) => ({ name, [key]: val }))
    .sort((a, b) => b[key] - a[key])
    .slice(0, 10)

  return {
    modelRanking: toRankList(modelMap, 'quota'),
    channelRanking: toRankList(channelMap, 'quota'),
    userRanking: toRankList(userMap, 'quota'),
    errorModelRanking: toRankList(errorModelMap, 'count')
  }
})

const rankClass = (idx) => {
  if (idx === 0) return 'gold'
  if (idx === 1) return 'silver'
  if (idx === 2) return 'bronze'
  return ''
}

const updateRanking = () => {}

const loadSites = async () => {
  try {
    const res = await axios.get('/api/upstream-sites', { headers: authHeaders() })
    availableSites.value = res.data || []
  } catch { /* ignore */ }
}

const loadStats = async () => {
  loadingStats.value = true
  errorMsg.value = ''
  try {
    const res = await axios.get('/api/dashboard/stats', {
      params: { date: selectedDate.value },
      headers: authHeaders()
    })
    todayStatsData.value = res.data.todayStats || []
    yesterdayStatsData.value = res.data.yesterdayStats || []
  } catch (e) {
    errorMsg.value = e.response?.data?.error || '加载统计数据失败'
  } finally {
    loadingStats.value = false
  }
}

const computeToday = async () => {
  computing.value = true
  errorMsg.value = ''
  computeSteps.value = []
  startPolling()
  try {
    const body = { date: selectedDate.value, force: true }
    if (selectedComputeSiteId.value) {
      body.siteId = selectedComputeSiteId.value
    }
    const res = await axios.post('/api/dashboard/compute',
      body,
      { headers: authHeaders(), timeout: 600000 }
    )
    ElMessage.success(`计算完成: ${res.data.computed} 条记录`)
    if (res.data.errors?.length) {
      ElMessage.warning(`${res.data.errors.length} 个错误`)
    }
    await pollComputeStatus()
    await loadStats()
  } catch (e) {
    errorMsg.value = e.response?.data?.error || '计算失败'
  } finally {
    computing.value = false
    stopPolling()
  }
}

const loadConfig = async () => {
  try {
    const res = await axios.get('/api/dashboard/config', { headers: authHeaders() })
    startDate.value = res.data.startDate || ''
    concurrency.value = res.data.concurrency || 5
  } catch { /* ignore */ }
}

const saveConfig = async () => {
  if (!startDate.value) {
    ElMessage.warning('请选择开始日期')
    return
  }
  savingConfig.value = true
  try {
    await axios.put('/api/dashboard/config',
      { startDate: startDate.value, concurrency: concurrency.value },
      { headers: authHeaders() }
    )
    ElMessage.success('配置已保存')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    savingConfig.value = false
  }
}

const batchCompute = async () => {
  if (!startDate.value) {
    ElMessage.warning('请先设置开始计算日期')
    return
  }
  batchComputing.value = true
  batchResult.value = null
  errorMsg.value = ''
  try {
    const body = { startDate: startDate.value, endDate: selectedDate.value }
    if (batchSiteId.value) {
      body.siteId = batchSiteId.value
    }
    const res = await axios.post('/api/dashboard/compute',
      body,
      { headers: authHeaders(), timeout: 1800000 }
    )
    batchResult.value = res.data
    ElMessage.success(`批量计算完成: ${res.data.computed} 条记录`)
    await loadStats()
  } catch (e) {
    errorMsg.value = e.response?.data?.error || '批量计算失败'
  } finally {
    batchComputing.value = false
  }
}

const formatTime = (t) => {
  if (!t) return ''
  const d = new Date(t)
  if (isNaN(d.getTime())) return t
  return d.toLocaleString('zh-CN', { hour12: false })
}

const loadNotifyConfig = async () => {
  try {
    const res = await axios.get('/api/dashboard/notification', { headers: authHeaders() })
    notifyConfig.value = {
      enabled: res.data.enabled || false,
      notification_type: res.data.notification_type || 'feishu',
      webhook_url: res.data.webhook_url || '',
      sign_key: res.data.sign_key || '',
      wework_webhook_url: res.data.wework_webhook_url || '',
      push_time: res.data.push_time || '08:00',
      top_n: res.data.top_n || 10,
      auto_compute: res.data.auto_compute || false,
      last_attempt_at: res.data.last_attempt_at,
      last_sent_at: res.data.last_sent_at,
      last_sent_date: res.data.last_sent_date || '',
      last_error: res.data.last_error || ''
    }
  } catch { /* ignore */ }
}

const openNotifyDialog = async () => {
  await loadNotifyConfig()
  notifyDialogVisible.value = true
}

const saveNotifyConfig = async () => {
  savingNotify.value = true
  try {
    await axios.put('/api/dashboard/notification', notifyConfig.value, { headers: authHeaders() })
    ElMessage.success('推送配置已保存')
    notifyDialogVisible.value = false
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    savingNotify.value = false
  }
}

const testNotify = async () => {
  testingNotify.value = true
  try {
    await axios.post('/api/dashboard/notification/test', {}, { headers: authHeaders() })
    ElMessage.success('测试通知发送成功')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '测试通知发送失败')
  } finally {
    testingNotify.value = false
  }
}

const sendNow = async () => {
  pushingNow.value = true
  try {
    const date = selectedDate.value || yesterdayLocal()
    const res = await axios.post('/api/dashboard/notification/send-now', { date }, { headers: authHeaders(), timeout: 1800000 })
    ElMessage.success(res.data.message || '推送成功')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '推送失败')
  } finally {
    pushingNow.value = false
  }
}

const yesterdayLocal = () => {
  const d = new Date()
  d.setDate(d.getDate() - 1)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

onMounted(async () => {
  await loadSites()
  await loadConfig()
  await loadStats()
})
</script>

<style scoped>
.dashboard-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 10px;
}

.header h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
}

.actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.actions .label {
  font-size: 14px;
  color: #606266;
}

/* Consumption Cards */
.consumption-section {
  margin-bottom: 24px;
}

.site-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 16px;
}

.site-card {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  overflow: hidden;
  border-left: 4px solid #e6a23c;
}

.total-card {
  border-left-color: #409eff;
}

.site-card-header {
  padding: 10px 16px;
  font-size: 13px;
  font-weight: 600;
  color: #606266;
  border-bottom: 1px solid #f0f0f0;
  background: #fafafa;
}

.site-card-body {
  padding: 16px;
}

.consumption-value {
  font-size: 28px;
  font-weight: 700;
  color: #e6a23c;
  line-height: 1.3;
}

.consumption-value.total {
  color: #409eff;
}

.consumption-label {
  font-size: 13px;
  color: #909399;
  margin-top: 2px;
}

.consumption-compare {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
}

.yesterday-value {
  color: #909399;
}

.change-rate {
  font-weight: 600;
  font-size: 13px;
}

.change-rate.up {
  color: #f56c6c;
}

.change-rate.down {
  color: #67c23a;
}

.change-rate.flat {
  color: #909399;
}

/* Rankings */
.ranking-section {
  margin-top: 20px;
}

.ranking-header {
  margin-bottom: 16px;
}

.ranking-tables {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
}

.ranking-card {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  overflow: hidden;
}

.ranking-title {
  padding: 12px 16px;
  font-size: 15px;
  font-weight: 700;
  color: #303133;
  border-bottom: 1px solid #f0f0f0;
}

.ranking-list {
  padding: 8px 0;
}

.ranking-item {
  display: flex;
  align-items: center;
  padding: 8px 16px;
  gap: 10px;
}

.ranking-item:hover {
  background: #f5f7fa;
}

.rank-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  font-size: 12px;
  font-weight: 700;
  background: #e5e7eb;
  color: #606266;
  flex-shrink: 0;
}

.rank-badge.gold {
  background: #ffc107;
  color: #fff;
}

.rank-badge.silver {
  background: #90a4ae;
  color: #fff;
}

.rank-badge.bronze {
  background: #cd7f32;
  color: #fff;
}

.rank-name {
  flex: 1;
  font-size: 13px;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rank-value {
  font-size: 13px;
  font-weight: 600;
  color: #e6a23c;
  flex-shrink: 0;
}

.rank-value.error-count {
  color: #f56c6c;
}

.ranking-empty {
  padding: 20px 16px;
  text-align: center;
  color: #c0c4cc;
  font-size: 13px;
}

/* Config Section */
.config-section {
  margin-top: 12px;
}

.config-section h3 {
  font-size: 16px;
  margin: 0 0 12px 0;
  font-weight: 600;
}

.config-form {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 16px 20px;
}

.config-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.config-label {
  font-size: 14px;
  color: #606266;
  flex-shrink: 0;
}

.config-hint {
  font-size: 12px;
  color: #c0c4cc;
}

@media (max-width: 1200px) {
  .ranking-tables {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 768px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
  }
  .actions {
    flex-direction: column;
    width: 100%;
  }
  .site-cards {
    grid-template-columns: 1fr;
  }
  .ranking-tables {
    grid-template-columns: 1fr;
  }
  .config-row {
    flex-direction: column;
    align-items: flex-start;
  }
}

.step-progress-section {
  background: #f5f7fa;
  border-radius: 8px;
  padding: 12px 16px;
}

.step-progress-card {
  margin-bottom: 8px;
}

.step-progress-title {
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 6px;
  color: #303133;
}

.step-progress-list {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}

.step-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
}

.step-icon {
  font-size: 14px;
}

.step-name {
  color: #606266;
}

.step-error {
  color: #f56c6c;
  font-size: 12px;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
