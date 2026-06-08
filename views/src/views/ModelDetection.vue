<template>
  <div class="model-detection-page">
    <section class="toolbar-panel">
      <div class="toolbar-left">
        <el-button type="primary" :icon="VideoPlay" :loading="running" @click="runDetection">
          发起检测
        </el-button>
        <el-button :icon="Refresh" :loading="loadingSites || loadingJobs" @click="refreshAll">
          刷新
        </el-button>
        <el-button :icon="Setting" @click="openGlobalConfigDialog">
          检测与推送设置
        </el-button>
      </div>
      <div class="toolbar-summary">
        已配置 {{ enabledSiteCount }} 个渠道，{{ enabledTargetCount }} 个检测模型
      </div>
    </section>

    <el-alert
      v-if="globalConfig.autoDetectEnabled"
      class="status-alert"
      type="success"
      :closable="false"
      :title="autoDetectionStatusText"
    />

    <section class="content-grid">
      <div class="panel sites-panel">
        <div class="panel-header">
          <div>
            <h2>渠道检测配置</h2>
            <span>为每个渠道配置 Veridrop 检测时使用的 API Key 和模型列表</span>
          </div>
          <el-input
            v-model="siteKeyword"
            class="site-search"
            clearable
            :prefix-icon="Search"
            placeholder="搜索渠道"
          />
        </div>

        <el-table
          :data="filteredSites"
          border
          v-loading="loadingSites"
          height="520"
          highlight-current-row
          @row-click="selectSite"
        >
          <el-table-column label="渠道" min-width="220">
            <template #default="{ row }">
              <div class="site-cell">
                <strong>{{ row.name || row.url || '-' }}</strong>
                <span>{{ row.url || '-' }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="channelId" label="ID" width="80" />
          <el-table-column label="检测" width="95">
            <template #default="{ row }">
              <el-tag :type="row.modelDetection?.enabled ? 'success' : 'info'">
                {{ row.modelDetection?.enabled ? '启用' : '未启用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="模型数" width="90">
            <template #default="{ row }">
              {{ enabledTargetTotal(row.modelDetection) }}
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150">
            <template #default="{ row }">
              <div class="row-actions">
                <el-tooltip content="测试该渠道" placement="top">
                  <el-button
                    size="small"
                    circle
                    :icon="VideoPlay"
                    :loading="isTestingSite(row)"
                    :disabled="!siteCanRunDetection(row)"
                    @click.stop="runSiteDetection(row)"
                  />
                </el-tooltip>
                <el-button size="small" @click.stop="openSiteConfig(row)">配置</el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="panel jobs-panel">
        <div class="panel-header">
          <div>
            <h2>检测报告</h2>
            <span>后台轮询 Veridrop 后持久化的最近检测结果</span>
          </div>
          <el-select v-model="jobSiteFilter" clearable placeholder="全部渠道" class="job-filter">
            <el-option
              v-for="site in sites"
              :key="site.id"
              :label="site.name || site.url || site.id"
              :value="site.id"
            />
          </el-select>
        </div>

        <el-table :data="jobs" border v-loading="loadingJobs" height="520">
          <el-table-column label="渠道 / 模型" min-width="220">
            <template #default="{ row }">
              <div class="site-cell">
                <strong>{{ row.siteName || '-' }}</strong>
                <span>{{ protocolLabel(row.protocol) }} / {{ row.model }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="105">
            <template #default="{ row }">
              <el-tag :type="jobStatusType(row)">{{ jobStatusLabel(row) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="分数" width="90">
            <template #default="{ row }">
              {{ formatScore(row.totalScore) }}
            </template>
          </el-table-column>
          <el-table-column label="等级" width="130">
            <template #default="{ row }">
              {{ row.tierTitle || row.tier || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="完成时间" width="175">
            <template #default="{ row }">
              {{ formatDateTime(row.finishedAt || row.updatedAt) }}
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <div class="row-actions">
                <el-tooltip content="查看报告" placement="top">
                  <el-button
                    v-if="row.resultUrl || row.jsonUrl"
                    size="small"
                    circle
                    :icon="Document"
                    @click="openReportDialog(row)"
                  />
                </el-tooltip>
                <el-tooltip content="手动推送" placement="top">
                  <el-button
                    size="small"
                    circle
                    type="primary"
                    :icon="Promotion"
                    :loading="isPushingJob(row)"
                    :disabled="!jobCanPush(row)"
                    @click="pushReport(row)"
                  />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </section>

    <el-dialog title="模型检测配置" v-model="siteDialogVisible" width="820px">
      <el-form :model="siteForm" label-width="130px" v-loading="savingSite">
        <el-form-item label="渠道">
          <span>{{ selectedSite?.name || selectedSite?.url || '-' }}</span>
        </el-form-item>
        <el-form-item label="启用检测">
          <el-switch v-model="siteForm.enabled" />
        </el-form-item>
        <el-form-item label="检测 API Key">
          <el-input
            v-model="siteForm.apiKey"
            type="password"
            show-password
            placeholder="用于请求该渠道模型接口的 API Key"
          />
        </el-form-item>
        <el-form-item label="检测模型">
          <div class="target-list">
            <div
              v-for="(target, index) in siteForm.targets"
              :key="target.id"
              class="target-row"
            >
              <el-switch v-model="target.enabled" />
              <el-select v-model="target.protocol" class="target-protocol">
                <el-option label="Claude" value="claude" />
                <el-option label="OpenAI" value="openai" />
                <el-option label="Gemini" value="gemini" />
              </el-select>
              <el-input v-model="target.model" class="target-model" placeholder="模型名称" />
              <el-select v-model="target.mode" class="target-mode">
                <el-option label="quick" value="quick" />
                <el-option label="standard" value="standard" />
                <el-option label="full" value="full" />
              </el-select>
              <el-button type="danger" link @click="removeTarget(index)">删除</el-button>
              <el-input
                v-model="target.baseUrl"
                class="target-base-url"
                placeholder="Base URL，留空默认使用渠道地址"
              />
              <div class="target-options">
                <el-checkbox
                  v-if="target.protocol !== 'gemini'"
                  v-model="target.includeLongContext"
                >
                  长上下文
                </el-checkbox>
                <el-checkbox
                  v-if="target.protocol !== 'gemini'"
                  v-model="target.includeLongContextExtreme"
                >
                  极限长上下文
                </el-checkbox>
                <el-checkbox v-model="target.force">跳过预检</el-checkbox>
              </div>
            </div>
            <el-button :icon="Plus" @click="addTarget">添加模型</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="siteDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="savingSite" @click="saveSiteConfig">保存配置</el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog title="检测与推送设置" v-model="globalDialogVisible" width="720px">
      <el-form :model="globalConfig" label-width="150px" v-loading="loadingGlobalConfig">
        <el-form-item label="Veridrop 地址">
          <el-input v-model="globalConfig.veridropUrl" placeholder="http://127.0.0.1:8080" />
        </el-form-item>
        <el-form-item label="Veridrop Token">
          <el-input
            v-model="globalConfig.veridropApiToken"
            type="password"
            show-password
            placeholder="系统 API Token，可选"
          />
        </el-form-item>
        <el-form-item label="报告访问地址">
          <el-input v-model="globalConfig.reportBaseUrl" placeholder="https://your-balance.example.com" />
        </el-form-item>
        <el-divider />
        <el-form-item label="启用完成推送">
          <el-switch v-model="globalConfig.enabled" />
        </el-form-item>
        <el-form-item label="推送策略">
          <el-radio-group v-model="globalConfig.pushPolicy">
            <el-radio value="all">推送全部检测结果</el-radio>
            <el-radio value="failures">仅推送异常或未通过</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="通知方式">
          <el-radio-group v-model="globalConfig.notification_type">
            <el-radio value="feishu">飞书机器人</el-radio>
            <el-radio value="wework">企业微信机器人</el-radio>
          </el-radio-group>
        </el-form-item>
        <template v-if="globalConfig.notification_type === 'feishu'">
          <el-form-item label="飞书 Webhook">
            <el-input v-model="globalConfig.webhook_url" placeholder="飞书自定义机器人 Webhook URL" />
          </el-form-item>
          <el-form-item label="飞书签名密钥">
            <el-input v-model="globalConfig.sign_key" type="password" show-password placeholder="可选" />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item label="企业微信 Webhook">
            <el-input
              v-model="globalConfig.wework_webhook_url"
              placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxxx"
            />
          </el-form-item>
        </template>
        <el-divider />
        <el-form-item label="启用自动检测">
          <el-switch v-model="globalConfig.autoDetectEnabled" />
        </el-form-item>
        <el-form-item label="默认检测间隔">
          <el-input-number
            v-model="globalConfig.interval_minutes"
            :min="1"
            :max="10080"
            :step="60"
            controls-position="right"
          />
          <span class="form-suffix">分钟</span>
        </el-form-item>
        <el-form-item label="检测计划">
          <div class="schedule-list">
            <div
              v-for="(schedule, index) in globalConfig.schedules"
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
                :step="60"
                controls-position="right"
                style="width: 130px"
              />
              <span class="form-suffix">分钟</span>
              <el-button type="danger" link @click="removeSchedule(index)">删除</el-button>
            </div>
            <el-button @click="addSchedule">添加计划</el-button>
          </div>
        </el-form-item>
        <el-form-item label="上次自动检测">
          <span>{{ formatDateTime(globalConfig.last_auto_run_at) }}</span>
        </el-form-item>
        <el-form-item label="上次推送">
          <span>{{ formatDateTime(globalConfig.last_sent_at) }}</span>
        </el-form-item>
        <el-form-item v-if="globalConfig.last_error" label="最近错误">
          <el-alert type="error" :closable="false" :title="globalConfig.last_error" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="globalDialogVisible = false">取消</el-button>
          <el-button :loading="testingNotification" @click="testNotification">测试通知</el-button>
          <el-button type="primary" :loading="savingGlobalConfig" @click="saveGlobalConfig">
            保存设置
          </el-button>
        </span>
      </template>
    </el-dialog>

    <el-dialog title="检测报告" v-model="reportDialogVisible" width="900px">
      <div v-loading="loadingReport" class="report-dialog">
        <template v-if="activeReportJob">
          <div class="report-summary">
            <div>
              <span>渠道</span>
              <strong>{{ activeReportJob.siteName || '-' }}</strong>
            </div>
            <div>
              <span>模型</span>
              <strong>{{ protocolLabel(activeReportJob.protocol) }} / {{ activeReportJob.model }}</strong>
            </div>
            <div>
              <span>结论</span>
              <el-tag :type="jobStatusType(activeReportJob)">{{ jobStatusLabel(activeReportJob) }}</el-tag>
            </div>
            <div>
              <span>分数</span>
              <strong>{{ formatScore(activeReportJob.totalScore) }}</strong>
            </div>
            <div>
              <span>等级</span>
              <strong>{{ activeReportJob.tierTitle || activeReportJob.tier || '-' }}</strong>
            </div>
            <div>
              <span>完成时间</span>
              <strong>{{ formatDateTime(activeReportJob.finishedAt || activeReportJob.updatedAt) }}</strong>
            </div>
          </div>

          <el-alert
            v-if="activeReportJob.error"
            class="report-alert"
            type="error"
            :closable="false"
            :title="activeReportJob.error"
          />

          <div class="report-block">
            <h3>摘要</h3>
            <p>{{ activeReport?.summary || activeReportJob.summary || '暂无摘要' }}</p>
          </div>

          <div class="report-block" v-if="activeReport?.tier_message">
            <h3>等级说明</h3>
            <p>{{ activeReport.tier_message }}</p>
          </div>

          <div class="report-block" v-if="activeReportResults.length">
            <h3>检测项</h3>
            <el-table :data="activeReportResults" border max-height="360">
              <el-table-column prop="name" label="项目" min-width="180" />
              <el-table-column label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="resultStatusType(row.status)">{{ row.status || '-' }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="分数" width="90">
                <template #default="{ row }">
                  {{ formatScore(row.score) }}
                </template>
              </el-table-column>
              <el-table-column prop="summary" label="摘要" min-width="260" />
            </el-table>
          </div>

          <div class="report-block" v-if="activeReport?.self_reported_identity">
            <h3>模型自报身份</h3>
            <p>{{ activeReport.self_reported_identity }}</p>
          </div>
        </template>
      </div>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="reportDialogVisible = false">关闭</el-button>
          <el-button
            v-if="activeReportJob?.resultUrl"
            type="primary"
            @click="openExternalReport"
          >
            打开外部 HTML 报告
          </el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Plus, Promotion, Refresh, Search, Setting, VideoPlay } from '@element-plus/icons-vue'
import axios from 'axios'

const route = useRoute()
const sites = ref([])
const jobs = ref([])
const loadingSites = ref(false)
const loadingJobs = ref(false)
const running = ref(false)
const testingSiteIds = ref(new Set())
const pushingJobIds = ref(new Set())
const siteKeyword = ref('')
const jobSiteFilter = ref('')

const siteDialogVisible = ref(false)
const selectedSite = ref(null)
const siteForm = ref(defaultSiteModelDetection())
const savingSite = ref(false)

const globalDialogVisible = ref(false)
const globalConfig = ref(defaultGlobalConfig())
const loadingGlobalConfig = ref(false)
const savingGlobalConfig = ref(false)
const testingNotification = ref(false)
const reportDialogVisible = ref(false)
const loadingReport = ref(false)
const activeReportJob = ref(null)
const activeReport = ref(null)

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const enabledSiteCount = computed(() => (
  sites.value.filter(site => site.modelDetection?.enabled).length
))

const enabledTargetCount = computed(() => (
  sites.value.reduce((total, site) => total + enabledTargetTotal(site.modelDetection), 0)
))

const filteredSites = computed(() => {
  const keyword = siteKeyword.value.trim().toLowerCase()
  if (!keyword) return sites.value
  return sites.value.filter(site => [
    site.name,
    site.url,
    String(site.channelId || '')
  ].some(value => (value || '').toLowerCase().includes(keyword)))
})

const autoDetectionStatusText = computed(() => {
  const scheduleCount = globalConfig.value.schedules?.length || 0
  const intervalText = scheduleCount > 0
    ? `${scheduleCount} 个检测计划`
    : `每 ${globalConfig.value.interval_minutes} 分钟`
  return `自动检测已启用，${intervalText}提交一次已启用的检测模型`
})

const activeReportResults = computed(() => (
  Array.isArray(activeReport.value?.results) ? activeReport.value.results : []
))

watch(jobSiteFilter, () => {
  fetchJobs()
})

watch(() => route.query.jobId, () => {
  openReportFromRoute()
})

function defaultSiteModelDetection() {
  return {
    enabled: false,
    apiKey: '',
    targets: []
  }
}

function defaultTarget(protocol = 'openai') {
  return {
    id: crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`,
    enabled: true,
    protocol,
    model: '',
    baseUrl: '',
    mode: protocol === 'claude' ? 'full' : 'standard',
    includeLongContext: false,
    includeLongContextExtreme: false,
    force: false
  }
}

function defaultGlobalConfig() {
  return {
    enabled: false,
    autoDetectEnabled: false,
    veridropUrl: 'http://127.0.0.1:8080',
    veridropApiToken: '',
    reportBaseUrl: defaultReportBaseUrl(),
    notification_type: 'feishu',
    webhook_url: '',
    sign_key: '',
    wework_webhook_url: '',
    interval_minutes: 1440,
    schedules: [],
    pushPolicy: 'all',
    last_auto_run_at: '',
    last_attempt_at: '',
    last_sent_at: '',
    last_error: ''
  }
}

function defaultReportBaseUrl() {
  return window.location.origin
}

function normalizeSite(site) {
  return {
    ...site,
    modelDetection: normalizeSiteConfig(site.modelDetection)
  }
}

function normalizeSiteConfig(config = {}) {
  return {
    ...defaultSiteModelDetection(),
    ...config,
    apiKey: config?.apiKey || '',
    targets: (config?.targets || []).map(target => ({
      ...defaultTarget(target.protocol || 'openai'),
      ...target,
      protocol: normalizeProtocol(target.protocol),
      mode: target.mode || defaultTarget(target.protocol || 'openai').mode,
      baseUrl: target.baseUrl || '',
      includeLongContext: Boolean(target.includeLongContext),
      includeLongContextExtreme: Boolean(target.includeLongContextExtreme),
      force: Boolean(target.force)
    }))
  }
}

function normalizeGlobalConfig(config = {}) {
  const defaults = defaultGlobalConfig()
  return {
    ...defaults,
    ...config,
    reportBaseUrl: config?.reportBaseUrl || defaults.reportBaseUrl,
    schedules: (config?.schedules || []).map(schedule => ({
      start_time: schedule.start_time || '',
      end_time: schedule.end_time || '',
      interval_minutes: Number(schedule.interval_minutes || 1440)
    }))
  }
}

async function fetchSites() {
  loadingSites.value = true
  try {
    const res = await axios.get('/api/sites', { headers: authHeaders() })
    sites.value = (res.data || []).map(normalizeSite)
    if (!selectedSite.value && sites.value.length) {
      selectedSite.value = sites.value[0]
    }
  } catch (err) {
    handleAuthOrError(err, '无法获取站点列表')
  } finally {
    loadingSites.value = false
  }
}

async function fetchJobs() {
  loadingJobs.value = true
  try {
    const params = { limit: 100 }
    if (jobSiteFilter.value) {
      params.siteId = jobSiteFilter.value
    }
    const res = await axios.get('/api/model-detection/jobs', {
      headers: authHeaders(),
      params
    })
    jobs.value = res.data || []
  } catch (err) {
    handleAuthOrError(err, '无法获取检测报告')
  } finally {
    loadingJobs.value = false
  }
}

async function fetchGlobalConfig() {
  try {
    const res = await axios.get('/api/model-detection/config', { headers: authHeaders() })
    globalConfig.value = normalizeGlobalConfig(res.data)
  } catch (err) {
    handleAuthOrError(err, '无法获取模型检测配置')
  }
}

async function refreshAll() {
  await Promise.all([fetchSites(), fetchJobs(), fetchGlobalConfig()])
}

function selectSite(site) {
  selectedSite.value = site
}

function openSiteConfig(site) {
  selectedSite.value = site
  siteForm.value = normalizeSiteConfig(site.modelDetection)
  if (!siteForm.value.targets.length) {
    siteForm.value.targets = [defaultTarget('openai')]
  }
  siteDialogVisible.value = true
}

function addTarget() {
  siteForm.value.targets = [...siteForm.value.targets, defaultTarget('openai')]
}

function removeTarget(index) {
  siteForm.value.targets = siteForm.value.targets.filter((_, targetIndex) => targetIndex !== index)
}

async function saveSiteConfig() {
  if (!selectedSite.value?.id) return
  savingSite.value = true
  try {
    const res = await axios.put(
      `/api/sites/${selectedSite.value.id}/model-detection`,
      siteConfigPayload(),
      { headers: authHeaders() }
    )
    const siteIndex = sites.value.findIndex(site => site.id === selectedSite.value.id)
    if (siteIndex >= 0) {
      sites.value[siteIndex].modelDetection = normalizeSiteConfig(res.data)
    }
    siteDialogVisible.value = false
    ElMessage.success('模型检测配置已保存')
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '模型检测配置保存失败')
  } finally {
    savingSite.value = false
  }
}

function siteConfigPayload() {
  return {
    enabled: Boolean(siteForm.value.enabled),
    apiKey: siteForm.value.apiKey.trim(),
    targets: siteForm.value.targets.map(target => ({
      id: target.id,
      enabled: Boolean(target.enabled),
      protocol: normalizeProtocol(target.protocol),
      model: target.model.trim(),
      baseUrl: target.baseUrl.trim(),
      mode: target.mode || 'standard',
      includeLongContext: Boolean(target.includeLongContext),
      includeLongContextExtreme: Boolean(target.includeLongContextExtreme),
      force: Boolean(target.force)
    }))
  }
}

function openGlobalConfigDialog() {
  globalDialogVisible.value = true
  loadingGlobalConfig.value = true
  fetchGlobalConfig().finally(() => {
    loadingGlobalConfig.value = false
  })
}

async function saveGlobalConfig() {
  savingGlobalConfig.value = true
  try {
    const res = await axios.put(
      '/api/model-detection/config',
      globalConfigPayload(),
      { headers: authHeaders() }
    )
    globalConfig.value = normalizeGlobalConfig(res.data)
    ElMessage.success('检测与推送设置已保存')
    return true
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '检测与推送设置保存失败')
    return false
  } finally {
    savingGlobalConfig.value = false
  }
}

function globalConfigPayload() {
  return {
    enabled: Boolean(globalConfig.value.enabled),
    autoDetectEnabled: Boolean(globalConfig.value.autoDetectEnabled),
    veridropUrl: globalConfig.value.veridropUrl.trim(),
    veridropApiToken: globalConfig.value.veridropApiToken.trim(),
    reportBaseUrl: globalConfig.value.reportBaseUrl.trim(),
    notification_type: globalConfig.value.notification_type,
    webhook_url: globalConfig.value.webhook_url.trim(),
    sign_key: globalConfig.value.sign_key.trim(),
    wework_webhook_url: globalConfig.value.wework_webhook_url.trim(),
    interval_minutes: Number(globalConfig.value.interval_minutes || 1440),
    schedules: globalConfig.value.schedules.map(schedule => ({
      start_time: schedule.start_time || '',
      end_time: schedule.end_time || '',
      interval_minutes: Number(schedule.interval_minutes || 0)
    })),
    pushPolicy: globalConfig.value.pushPolicy
  }
}

async function testNotification() {
  testingNotification.value = true
  try {
    const saved = await saveGlobalConfig()
    if (!saved) return
    const res = await axios.post('/api/model-detection/test-notification', {}, { headers: authHeaders() })
    if (res.data?.success) {
      ElMessage.success(res.data.message || '模型检测通知测试成功')
    } else {
      ElMessage.error(res.data?.message || '模型检测通知测试失败')
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.message || '模型检测通知测试失败')
  } finally {
    testingNotification.value = false
  }
}

async function runDetection() {
  try {
    await ElMessageBox.confirm(
      '将为所有已启用的渠道和检测模型提交 Veridrop 异步检测任务。确认继续吗?',
      '发起模型检测',
      { type: 'warning' }
    )
  } catch {
    return
  }

  running.value = true
  try {
    const res = await axios.post('/api/model-detection/run', {}, { headers: authHeaders() })
    const created = res.data?.created_count || 0
    const errors = res.data?.error_count || 0
    if (created > 0) {
      ElMessage.success(`已提交 ${created} 个检测任务${errors ? `，${errors} 个配置未提交` : ''}`)
    } else {
      ElMessage.warning(errors ? `未提交任务：${(res.data?.errors || []).join('；')}` : '没有可提交的检测模型')
    }
    await fetchJobs()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '提交模型检测失败')
  } finally {
    running.value = false
  }
}

async function runSiteDetection(site) {
  if (!site?.id) return
  if (!siteCanRunDetection(site)) {
    ElMessage.warning('请先启用该渠道并配置至少一个检测模型')
    return
  }

  setTestingSite(site.id, true)
  try {
    const res = await axios.post(
      '/api/model-detection/run',
      { siteIds: [site.id] },
      { headers: authHeaders() }
    )
    const created = res.data?.created_count || 0
    const errors = res.data?.error_count || 0
    if (created > 0) {
      ElMessage.success(`已为该渠道提交 ${created} 个检测任务${errors ? `，${errors} 个配置未提交` : ''}`)
    } else {
      ElMessage.warning(errors ? `未提交任务：${(res.data?.errors || []).join('；')}` : '该渠道没有可提交的检测模型')
    }
    jobSiteFilter.value = site.id
    await fetchJobs()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '提交该渠道检测失败')
  } finally {
    setTestingSite(site.id, false)
  }
}

function siteCanRunDetection(site) {
  return Boolean(site?.modelDetection?.enabled && enabledTargetTotal(site.modelDetection) > 0)
}

function isTestingSite(site) {
  return testingSiteIds.value.has(site?.id)
}

function setTestingSite(siteId, value) {
  const next = new Set(testingSiteIds.value)
  if (value) {
    next.add(siteId)
  } else {
    next.delete(siteId)
  }
  testingSiteIds.value = next
}

function addSchedule() {
  const schedules = globalConfig.value.schedules || []
  const startTime = schedules[schedules.length - 1]?.end_time || '02:00'
  globalConfig.value.schedules = [
    ...schedules,
    {
      start_time: startTime,
      end_time: addMinutesToTime(startTime, 120),
      interval_minutes: Number(globalConfig.value.interval_minutes || 1440)
    }
  ]
}

function removeSchedule(index) {
  globalConfig.value.schedules = globalConfig.value.schedules.filter((_, scheduleIndex) => scheduleIndex !== index)
}

function addMinutesToTime(timeText, minutesToAdd) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(timeText || '')
  if (!match) return '04:00'
  const total = ((Number(match[1]) * 60 + Number(match[2]) + minutesToAdd) % (24 * 60) + (24 * 60)) % (24 * 60)
  return `${String(Math.floor(total / 60)).padStart(2, '0')}:${String(total % 60).padStart(2, '0')}`
}

function enabledTargetTotal(config) {
  return (config?.targets || []).filter(target => target.enabled).length
}

function normalizeProtocol(protocol) {
  const value = (protocol || '').toLowerCase()
  if (value === 'anthropic') return 'claude'
  if (['claude', 'openai', 'gemini'].includes(value)) return value
  return 'openai'
}

function protocolLabel(protocol) {
  const value = normalizeProtocol(protocol)
  if (value === 'claude') return 'Claude'
  if (value === 'gemini') return 'Gemini'
  return 'OpenAI'
}

function jobStatusLabel(job) {
  if (job.status === 'done') {
    if (job.verdict === 'passed') return '通过'
    if (job.verdict === 'marginal') return '存疑'
    if (job.verdict === 'failed') return '未通过'
    return '完成'
  }
  if (job.status === 'error') return '异常'
  if (job.status === 'running') return '运行中'
  if (job.status === 'queued') return '排队中'
  return job.status || '-'
}

function jobStatusType(job) {
  if (job.status === 'error' || job.verdict === 'failed') return 'danger'
  if (job.verdict === 'marginal') return 'warning'
  if (job.status === 'done') return 'success'
  if (job.status === 'running') return 'primary'
  return 'info'
}

function formatScore(score) {
  const value = Number(score || 0)
  return value > 0 ? value.toFixed(1) : '-'
}

function formatDateTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

async function openReportDialog(row) {
  activeReportJob.value = row
  activeReport.value = row.report || null
  reportDialogVisible.value = true
  loadingReport.value = true
  try {
    const res = await axios.get(`/api/model-detection/jobs/${row.id}/report`, {
      headers: authHeaders()
    })
    activeReportJob.value = res.data?.job || row
    activeReport.value = res.data?.report || null
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '读取检测报告失败')
  } finally {
    loadingReport.value = false
  }
}

async function openReportFromRoute() {
  const queryJobId = Array.isArray(route.query.jobId) ? route.query.jobId[0] : route.query.jobId
  const jobId = String(queryJobId || '').trim()
  if (!jobId) return
  if (reportDialogVisible.value && activeReportJob.value?.id === jobId) return

  const job = jobs.value.find(item => item.id === jobId) || { id: jobId }
  await openReportDialog(job)
}

async function pushReport(row) {
  if (!row?.id) return
  setPushingJob(row.id, true)
  try {
    const res = await axios.post(`/api/model-detection/jobs/${row.id}/push`, {}, {
      headers: authHeaders()
    })
    if (res.data?.success) {
      ElMessage.success(res.data.message || '模型检测报告已推送')
      await fetchJobs()
    } else {
      ElMessage.error(res.data?.message || '模型检测报告推送失败')
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.message || '模型检测报告推送失败')
  } finally {
    setPushingJob(row.id, false)
  }
}

function openExternalReport() {
  if (activeReportJob.value?.resultUrl) {
    window.open(activeReportJob.value.resultUrl, '_blank', 'noopener,noreferrer')
  }
}

function jobCanPush(job) {
  return job?.status === 'done' || job?.status === 'error'
}

function isPushingJob(job) {
  return pushingJobIds.value.has(job?.id)
}

function setPushingJob(jobId, value) {
  const next = new Set(pushingJobIds.value)
  if (value) {
    next.add(jobId)
  } else {
    next.delete(jobId)
  }
  pushingJobIds.value = next
}

function resultStatusType(status) {
  const value = (status || '').toLowerCase()
  if (value === 'pass' || value === 'passed') return 'success'
  if (value === 'fail' || value === 'failed') return 'danger'
  if (value === 'warn' || value === 'warning' || value === 'marginal') return 'warning'
  return 'info'
}

function handleAuthOrError(err, message) {
  if (err.response?.status === 401) {
    localStorage.removeItem('token')
    window.location.href = '/login'
    return
  }
  ElMessage.error(err.response?.data?.error || message)
}

onMounted(async () => {
  await refreshAll()
  await openReportFromRoute()
})
</script>

<style scoped>
.model-detection-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  text-align: left;
}

.toolbar-panel,
.panel {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #ffffff;
}

.toolbar-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
}

.toolbar-left {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.toolbar-summary {
  color: #6b7280;
  font-size: 14px;
}

.status-alert {
  margin: 0;
}

.content-grid {
  display: grid;
  grid-template-columns: minmax(420px, 1fr) minmax(520px, 1.2fr);
  gap: 16px;
  min-width: 0;
}

.panel {
  min-width: 0;
  padding: 16px;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.panel-header h2 {
  margin: 0;
  color: #111827;
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0;
}

.panel-header span {
  display: block;
  margin-top: 4px;
  color: #6b7280;
  font-size: 13px;
}

.site-search,
.job-filter {
  width: 220px;
  flex: 0 0 auto;
}

.site-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.site-cell strong,
.site-cell span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.site-cell strong {
  color: #111827;
  font-weight: 600;
}

.site-cell span {
  color: #6b7280;
  font-size: 12px;
}

.row-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.target-list,
.schedule-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.target-row {
  display: grid;
  grid-template-columns: 54px 120px minmax(160px, 1fr) 120px 56px;
  gap: 8px;
  align-items: center;
  padding: 10px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #f9fafb;
}

.target-base-url,
.target-options {
  grid-column: 2 / -1;
}

.target-options {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}

.schedule-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.schedule-separator,
.form-suffix {
  color: #606266;
}

.form-suffix {
  margin-left: 8px;
}

.report-dialog {
  min-height: 160px;
}

.report-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 16px;
}

.report-summary > div {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #f9fafb;
}

.report-summary span {
  display: block;
  margin-bottom: 4px;
  color: #6b7280;
  font-size: 12px;
}

.report-summary strong {
  display: block;
  overflow: hidden;
  color: #111827;
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.report-alert {
  margin-bottom: 16px;
}

.report-block {
  margin-top: 16px;
}

.report-block h3 {
  margin: 0 0 8px;
  color: #111827;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0;
}

.report-block p {
  margin: 0;
  color: #374151;
  font-size: 14px;
  line-height: 1.7;
  white-space: pre-wrap;
}

@media (max-width: 1200px) {
  .content-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .toolbar-panel,
  .panel-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .toolbar-left,
  .toolbar-left .el-button,
  .site-search,
  .job-filter {
    width: 100%;
  }

  .target-row {
    grid-template-columns: 1fr;
  }

  .report-summary {
    grid-template-columns: 1fr;
  }

  .target-base-url,
  .target-options {
    grid-column: auto;
  }
}
</style>
