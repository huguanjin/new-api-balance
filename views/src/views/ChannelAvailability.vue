<template>
  <div class="channel-availability-container">
    <div class="header">
      <div class="header-left">
        <h2>上游渠道可用性检测</h2>
        <el-select v-model="selectedSiteId" placeholder="请选择上游站点" style="width: 220px" @change="onSiteChange">
          <el-option v-for="s in siteList" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
        <el-button text type="primary" @click="$router.push('/upstream-sites')">管理站点</el-button>
      </div>
      <div class="actions">
        <el-button type="primary" @click="fetchChannels" :loading="fetching" :disabled="!selectedSiteId">获取渠道列表</el-button>
        <el-button type="success" @click="openBatchTestDialog" :disabled="!selectedChannels.length">批量测试 ({{ selectedChannels.length }})</el-button>
        <el-button type="warning" @click="batchEnableChannels" :loading="batchEnabling" :disabled="!selectedChannels.length">批量启用</el-button>
        <el-button type="danger" @click="batchDisableChannels" :loading="batchDisabling" :disabled="!selectedChannels.length">批量禁用</el-button>
        <el-button type="danger" plain @click="batchDeleteChannels" :loading="batchDeleting" :disabled="!selectedChannels.length">批量删除</el-button>
        <el-button type="info" @click="openNotifyConfigDialog" :disabled="!selectedSiteId">推送配置</el-button>
      </div>
    </div>

    <div class="table-toolbar">
      <el-select v-model="statusFilter" placeholder="状态筛选" style="width: 140px">
        <el-option label="全部状态" value="all" />
        <el-option label="启用" value="1" />
        <el-option label="禁用" value="2" />
      </el-select>
      <el-select v-model="groupFilter" placeholder="选择分组" clearable filterable style="width: 180px">
        <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
      </el-select>
      <el-select v-model="testResultFilter" placeholder="测试结果" style="width: 140px">
        <el-option label="全部结果" value="all" />
        <el-option label="测试成功" value="success" />
        <el-option label="测试失败" value="failed" />
        <el-option label="未测试" value="untested" />
      </el-select>
      <el-input
        v-model="searchText"
        placeholder="搜索渠道名称/模型/标签"
        clearable
        style="width: 240px"
      />
      <span class="filter-summary">显示 {{ filteredChannels.length }} / {{ channels.length }} 个渠道</span>
    </div>

    <el-table
      ref="tableRef"
      :data="filteredChannels"
      style="width: 100%"
      border
      v-loading="loading"
      @selection-change="handleSelectionChange"
      row-key="channelId"
    >
      <el-table-column type="selection" width="45" reserve-selection />
      <el-table-column prop="channelId" label="ID" width="70" sortable />
      <el-table-column label="状态" width="80">
        <template #default="{ row }">
          <el-tag :type="channelStatusType(row.status)" size="small">{{ channelStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="name" label="渠道名称" min-width="200" show-overflow-tooltip />
      <el-table-column prop="baseUrl" label="Base URL" min-width="180" show-overflow-tooltip />
      <el-table-column prop="group" label="分组" min-width="140" show-overflow-tooltip />
      <el-table-column prop="tag" label="标签" width="90" show-overflow-tooltip />
      <el-table-column prop="priority" label="优先级" width="80" sortable />
      <el-table-column prop="weight" label="权重" width="70" sortable />
      <el-table-column label="模型" min-width="200">
        <template #default="{ row }">
          <span class="models-text">{{ formatModels(row.models) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="测试结果" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.testResult === 'success'" type="success" size="small">成功</el-tag>
          <el-tag v-else-if="row.testResult === 'failed'" type="danger" size="small">失败</el-tag>
          <el-tag v-else type="info" size="small">未测试</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="响应时间" width="100" sortable :sort-method="sortByResponseTime">
        <template #default="{ row }">
          <span v-if="row.testedAt">{{ row.responseTime }} ms</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="测试错误" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.testError" class="error-text">{{ row.testError }}</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="150" fixed="right">
        <template #default="{ row }">
          <el-tooltip content="模型测试" placement="top">
            <el-button size="small" type="primary" link @click="openModelTestDialog(row)">测试</el-button>
          </el-tooltip>
          <el-tooltip content="快速测试" placement="top">
            <el-button
              size="small"
              circle
              :icon="CaretRight"
              :loading="isTestingChannel(row.channelId)"
              @click="testSingleChannel(row)"
            />
          </el-tooltip>
          <el-tooltip :content="formatCustomTestModels(row)" placement="top">
            <el-button
              size="small"
              circle
              :icon="Setting"
              :type="row.customTestModels && row.customTestModels.length ? 'warning' : 'default'"
              @click="openCustomTestModelDialog(row)"
            />
          </el-tooltip>
        </template>
      </el-table-column>
    </el-table>

    <!-- 默认测试模型设置弹窗 -->
    <el-dialog
      :title="`设置默认测试模型 - ${customModelChannelName}`"
      v-model="customModelDialogVisible"
      width="520px"
      :close-on-click-modal="false"
    >
      <el-alert
        class="model-test-alert"
        type="info"
        :closable="false"
        title="设置后批量测试将优先使用此处指定的模型；支持多选，可从下拉选择或手动输入"
      />
      <el-select
        v-model="customModelSelection"
        multiple
        filterable
        allow-create
        default-first-option
        placeholder="选择或输入测试模型"
        style="width: 100%"
      >
        <el-option v-for="m in customModelOptions" :key="m" :label="m" :value="m" />
      </el-select>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="customModelDialogVisible = false">取消</el-button>
          <el-button type="danger" link @click="customModelSelection = []" :disabled="!customModelSelection.length">清空</el-button>
          <el-button type="primary" @click="saveCustomTestModels" :loading="savingCustomModels">保存</el-button>
        </span>
      </template>
    </el-dialog>

    <!-- 批量测试弹窗 -->
    <el-dialog
      title="批量测试渠道"
      v-model="batchTestDialogVisible"
      width="500px"
      :close-on-click-modal="false"
    >
      <el-form label-width="100px">
        <el-form-item label="测试范围">
          <span>已选中 <strong>{{ selectedChannels.length }}</strong> 个渠道</span>
        </el-form-item>
        <el-form-item label="测试模型">
          <el-select
            v-model="batchTestModel"
            placeholder="留空则使用渠道默认模型"
            filterable
            allow-create
            clearable
            default-first-option
            style="width: 100%"
          >
            <el-option v-for="m in defaultTestModels" :key="m" :label="m" :value="m" />
          </el-select>
          <div class="form-tip">可从下拉列表选择，也可直接输入任意模型名称；留空则每个渠道使用各自的默认测试模型</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="batchTestDialogVisible = false">取消</el-button>
          <el-button type="primary" @click="confirmBatchTest" :loading="testingSelected">开始测试</el-button>
        </span>
      </template>
    </el-dialog>

    <!-- 模型测试弹窗 -->
    <el-dialog
      :title="modelTestDialogTitle"
      v-model="modelTestDialogVisible"
      width="720px"
      :close-on-click-modal="false"
    >
      <el-alert
        class="model-test-alert"
        type="info"
        :closable="false"
        title="说明：本页测试为非流式请求；若渠道仅支持流式返回，可能出现测试失败，请以实际使用为准。"
      />

      <div class="model-test-toolbar">
        <el-input
          v-model="modelSearchText"
          placeholder="搜索模型..."
          clearable
          :prefix-icon="Search"
          style="flex: 1"
        />
        <el-button link type="primary" @click="selectSuccessModels">选择成功</el-button>
      </div>

      <el-table
        ref="modelTableRef"
        :data="filteredModelList"
        style="width: 100%"
        border
        max-height="400"
        @selection-change="handleModelSelectionChange"
        row-key="model"
        size="small"
      >
        <el-table-column type="selection" width="40" reserve-selection />
        <el-table-column prop="model" label="模型名称" min-width="240" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'success'" type="success" size="small">成功</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger" size="small">失败</el-tag>
            <el-tag v-else-if="row.status === 'testing'" type="warning" size="small">测试中</el-tag>
            <el-tag v-else type="info" size="small">未开始</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="90">
          <template #default="{ row }">
            <span v-if="row.responseTime > 0">{{ row.responseTime }} ms</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="70">
          <template #default="{ row }">
            <el-button
              size="small"
              link
              type="primary"
              :loading="row.status === 'testing'"
              @click="testOneModelInDialog(row)"
            >测试</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="model-test-summary">
        显示第 1 条-第 {{ filteredModelList.length }} 条，共 {{ modelList.length }} 条
      </div>

      <template #footer>
        <span class="dialog-footer">
          <el-button @click="modelTestDialogVisible = false">取消</el-button>
          <el-button
            type="primary"
            @click="batchTestModelsInDialog"
            :loading="batchTestingModels"
            :disabled="!selectedModels.length"
          >
            批量测试{{ selectedModels.length ? ` ${selectedModels.length} 个模型` : '' }}
          </el-button>
        </span>
      </template>
    </el-dialog>

    <!-- 推送配置弹窗 -->
    <el-dialog title="推送配置" v-model="notifyConfigDialogVisible" width="620px" :close-on-click-modal="false">
      <el-alert
        class="config-alert"
        type="info"
        :closable="false"
        title="Webhook 留空则自动使用余额推送的机器人配置"
      />
      <el-form :model="notifyForm" label-width="130px" v-loading="notifyConfigLoading">
        <el-form-item label="启用">
          <el-switch v-model="notifyForm.enabled" />
        </el-form-item>
        <el-form-item label="通知类型">
          <el-radio-group v-model="notifyForm.notificationType">
            <el-radio value="feishu">飞书</el-radio>
            <el-radio value="wework">企业微信</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="notifyForm.notificationType === 'feishu'" label="飞书 Webhook">
          <el-input v-model="notifyForm.webhookUrl" placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/xxx" />
        </el-form-item>
        <el-form-item v-if="notifyForm.notificationType === 'feishu'" label="签名密钥">
          <el-input v-model="notifyForm.signKey" type="password" show-password placeholder="可选" />
        </el-form-item>
        <el-form-item v-if="notifyForm.notificationType === 'wework'" label="企微 Webhook">
          <el-input v-model="notifyForm.weworkWebhookUrl" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx" />
        </el-form-item>
        <el-form-item label="重新拉取渠道">
          <el-switch v-model="notifyForm.refreshChannels" />
          <div class="form-tip">开启后执行推送时会先拉取最新渠道数据；关闭则直接使用当前已存储的渠道信息</div>
        </el-form-item>
        <el-form-item label="渠道状态筛选">
          <el-select v-model="notifyForm.statusFilter" style="width: 180px">
            <el-option label="全部状态" :value="0" />
            <el-option label="仅启用" :value="1" />
            <el-option label="仅禁用" :value="2" />
          </el-select>
        </el-form-item>
        <el-form-item label="自动启停渠道">
          <el-switch v-model="notifyForm.autoToggle" />
          <div class="form-tip">开启后：已启用但测试失败的渠道将自动停用，已禁用但测试成功的渠道将自动启用</div>
        </el-form-item>
        <el-form-item label="响应超时停用">
          <el-input-number v-model="notifyForm.slowThresholdMs" :min="0" :step="1000" style="width: 180px" />
          <span style="margin-left: 8px; color: #606266; font-size: 13px">ms</span>
          <div class="form-tip">已启用渠道测试成功但响应时间超过此阈值时自动停用；设为 0 表示不开启</div>
        </el-form-item>
        <el-form-item label="推送计划">
          <div class="schedules-wrapper">
            <div v-for="(sch, idx) in notifyForm.schedules" :key="idx" class="schedule-row">
              <el-time-picker v-model="sch.startTimeObj" placeholder="开始" format="HH:mm" value-format="HH:mm" style="width: 110px" @change="v => sch.startTime = v" />
              <span class="schedule-sep">至</span>
              <el-time-picker v-model="sch.endTimeObj" placeholder="结束" format="HH:mm" value-format="HH:mm" style="width: 110px" @change="v => sch.endTime = v" />
              <span class="schedule-sep">每</span>
              <el-input-number v-model="sch.intervalMinutes" :min="1" :max="1440" size="small" style="width: 110px" />
              <span class="schedule-sep">分钟</span>
              <el-button :icon="Delete" type="danger" link size="small" @click="notifyForm.schedules.splice(idx, 1)" />
            </div>
            <el-button :icon="Plus" type="primary" link size="small" @click="addNotifySchedule">添加时间段</el-button>
          </div>
          <div class="form-tip">不配置推送计划时仅支持手动执行；配置后系统将按计划自动检测并推送</div>
        </el-form-item>
        <el-form-item label="生效渠道 ID">
          <div class="notify-channel-picker">
            <el-input
              v-model="notifyChannelSearch"
              placeholder="搜索渠道 ID / 名称 / 分组 / 标签"
              clearable
              :prefix-icon="Search"
              style="margin-bottom: 8px"
            />
            <div class="notify-channel-actions">
              <el-button type="primary" link size="small" @click="selectAllFilteredNotifyChannels">
                全选筛选结果 ({{ filteredNotifyChannels.length }})
              </el-button>
              <el-button type="primary" link size="small" @click="selectAllNotifyChannels">
                全选所有 ({{ channels.length }})
              </el-button>
              <el-button type="danger" link size="small" @click="notifyForm.channelIds = []" :disabled="!notifyForm.channelIds.length">
                清空
              </el-button>
              <el-button
                :type="notifyShowSelectedOnly ? 'warning' : 'info'"
                link size="small"
                @click="notifyShowSelectedOnly = !notifyShowSelectedOnly"
                :disabled="!notifyForm.channelIds.length"
              >
                {{ notifyShowSelectedOnly ? '查看全部' : '仅看已选' }}
              </el-button>
              <span class="notify-channel-count">已选 {{ notifyForm.channelIds.length }} 个</span>
            </div>
            <div class="notify-channel-list">
              <el-checkbox-group v-model="notifyForm.channelIds">
                <el-checkbox
                  v-for="ch in filteredNotifyChannels"
                  :key="ch.channelId"
                  :value="ch.channelId"
                  :label="`${ch.channelId} - ${ch.name}`"
                />
              </el-checkbox-group>
              <div v-if="filteredNotifyChannels.length === 0" class="notify-channel-empty">无匹配渠道</div>
            </div>
          </div>
          <div class="form-tip">
            至少配置一个渠道 ID，未在此列表中的渠道不会触发推送
          </div>
          <div v-if="notifyMissingIds.length" class="notify-missing-tags">
            <el-tag v-for="id in notifyMissingIds" :key="id" type="danger" size="small" style="margin-right: 6px">
              ID {{ id }} 不存在
            </el-tag>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="notifyConfigDialogVisible = false">取消</el-button>
          <el-button @click="testNotifyPush" :loading="testingNotify">测试推送</el-button>
          <el-button type="success" @click="runNotifyPush" :loading="runningNotify">执行推送</el-button>
          <el-button type="primary" @click="saveNotifyConfig" :loading="savingNotify">保存</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CaretRight, Search, Setting, Plus, Delete } from '@element-plus/icons-vue'
import axios from 'axios'

const defaultTestModels = [
  'claude-opus-4-6',
  'claude-opus-4-7',
  'claude-opus-4-8',
  'claude-sonnet-4-6',
  'gpt-5.5',
  'gpt-5.4'
]

const batchTestModel = ref('')
const batchTestDialogVisible = ref(false)
const channels = ref([])
const loading = ref(false)
const fetching = ref(false)
const testingSelected = ref(false)
const batchEnabling = ref(false)
const batchDisabling = ref(false)
const batchDeleting = ref(false)
const testingChannelIds = ref(new Set())
const selectedChannels = ref([])
const statusFilter = ref('all')
const groupFilter = ref('')
const testResultFilter = ref('all')
const searchText = ref('')
const groups = ref([])
const tableRef = ref(null)

const siteList = ref([])
const selectedSiteId = ref('')

const modelTestDialogVisible = ref(false)
const modelTestChannelId = ref(0)
const modelTestChannelName = ref('')
const modelList = ref([])
const selectedModels = ref([])
const modelSearchText = ref('')
const batchTestingModels = ref(false)
const modelTableRef = ref(null)

const notifyConfigDialogVisible = ref(false)
const notifyConfigLoading = ref(false)
const savingNotify = ref(false)
const testingNotify = ref(false)
const runningNotify = ref(false)
const notifyForm = ref({
  enabled: false,
  notificationType: 'feishu',
  webhookUrl: '',
  signKey: '',
  weworkWebhookUrl: '',
  channelIds: [],
  statusFilter: 0,
  refreshChannels: true,
  autoToggle: false,
  slowThresholdMs: 0,
  schedules: []
})
const notifyMissingIds = ref([])
const notifyChannelSearch = ref('')
const notifyShowSelectedOnly = ref(false)

const customModelDialogVisible = ref(false)
const customModelChannelId = ref(0)
const customModelChannelName = ref('')
const customModelSelection = ref([])
const customModelOptions = ref([])
const savingCustomModels = ref(false)

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const modelTestDialogTitle = computed(() => {
  const count = modelList.value.length
  return `${modelTestChannelName.value} 渠道的模型测试  共 ${count} 个模型`
})

const filteredModelList = computed(() => {
  if (!modelSearchText.value.trim()) return modelList.value
  const keyword = modelSearchText.value.trim().toLowerCase()
  return modelList.value.filter(m => m.model.toLowerCase().includes(keyword))
})

const filteredChannels = computed(() => {
  let list = channels.value
  if (statusFilter.value !== 'all') {
    const status = Number(statusFilter.value)
    list = list.filter(ch => ch.status === status)
  }
  if (groupFilter.value) {
    const keyword = groupFilter.value
    list = list.filter(ch => {
      const channelGroups = (ch.group || '').split(',').map(g => g.trim())
      return channelGroups.includes(keyword)
    })
  }
  if (testResultFilter.value !== 'all') {
    if (testResultFilter.value === 'success') {
      list = list.filter(ch => ch.testResult === 'success')
    } else if (testResultFilter.value === 'failed') {
      list = list.filter(ch => ch.testResult === 'failed')
    } else if (testResultFilter.value === 'untested') {
      list = list.filter(ch => !ch.testResult)
    }
  }
  if (searchText.value.trim()) {
    const keyword = searchText.value.trim().toLowerCase()
    list = list.filter(ch =>
      (ch.name || '').toLowerCase().includes(keyword) ||
      (ch.models || '').toLowerCase().includes(keyword) ||
      (ch.tag || '').toLowerCase().includes(keyword) ||
      (ch.group || '').toLowerCase().includes(keyword) ||
      String(ch.channelId).includes(keyword)
    )
  }
  return list
})

const channelStatusLabel = (status) => {
  if (status === 1) return '启用'
  if (status === 2) return '禁用'
  if (status === 3) return '限速'
  return `${status}`
}

const channelStatusType = (status) => {
  if (status === 1) return 'success'
  if (status === 2) return 'danger'
  if (status === 3) return 'warning'
  return 'info'
}

const formatModels = (models) => {
  if (!models) return '-'
  const list = models.split(',').map(m => m.trim()).filter(Boolean)
  if (list.length <= 3) return list.join(', ')
  return `${list.slice(0, 3).join(', ')} 等 ${list.length} 个`
}

const sortByResponseTime = (a, b) => (a.responseTime || 0) - (b.responseTime || 0)

const isTestingChannel = (channelId) => testingChannelIds.value.has(channelId)

const handleSelectionChange = (selection) => {
  selectedChannels.value = selection
}

const handleModelSelectionChange = (selection) => {
  selectedModels.value = selection
}

const fetchGroups = async () => {
  if (!selectedSiteId.value) return
  try {
    const res = await axios.get('/api/channel-availability/groups', {
      headers: authHeaders(),
      params: { upstreamSiteId: selectedSiteId.value }
    })
    groups.value = res.data?.groups || []
  } catch {
    // groups filter is optional
  }
}

const loadChannels = async () => {
  if (!selectedSiteId.value) {
    channels.value = []
    return
  }
  loading.value = true
  try {
    const res = await axios.get('/api/channel-availability/channels', {
      headers: authHeaders(),
      params: { upstreamSiteId: selectedSiteId.value }
    })
    channels.value = res.data || []
  } catch (err) {
    if (err.response?.status === 401) {
      logout()
    }
  } finally {
    loading.value = false
  }
}

const fetchChannels = async () => {
  fetching.value = true
  try {
    const res = await axios.post('/api/channel-availability/fetch', {
      upstreamSiteId: selectedSiteId.value
    }, {
      headers: authHeaders()
    })
    channels.value = res.data?.channels || []
    ElMessage.success(res.data?.message || '渠道列表已更新')
  } catch (err) {
    if (err.response?.status === 401) {
      logout()
      return
    }
    ElMessage.error(err.response?.data?.error || '获取渠道列表失败')
  } finally {
    fetching.value = false
  }
}

const openBatchTestDialog = () => {
  if (!selectedChannels.value.length) {
    ElMessage.warning('请先勾选要测试的渠道')
    return
  }
  batchTestModel.value = ''
  batchTestDialogVisible.value = true
}

const confirmBatchTest = async () => {
  testingSelected.value = true
  const ids = selectedChannels.value.map(ch => ch.channelId)
  try {
    const res = await axios.post('/api/channel-availability/test', {
      upstreamSiteId: selectedSiteId.value,
      channelIds: ids,
      testModel: batchTestModel.value || ''
    }, {
      headers: authHeaders()
    })
    applyTestResults(res.data?.results || [])
    ElMessage.success(res.data?.message || '测试完成')
    batchTestDialogVisible.value = false
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '批量测试失败')
  } finally {
    testingSelected.value = false
  }
}

const batchEnableChannels = async () => {
  if (!selectedChannels.value.length) return
  try {
    await ElMessageBox.confirm(
      `确认启用选中的 ${selectedChannels.value.length} 个渠道？`,
      '批量启用',
      { confirmButtonText: '启用', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  await batchUpdateStatus(1)
}

const batchDisableChannels = async () => {
  if (!selectedChannels.value.length) return
  try {
    await ElMessageBox.confirm(
      `确认禁用选中的 ${selectedChannels.value.length} 个渠道？`,
      '批量禁用',
      { confirmButtonText: '禁用', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  await batchUpdateStatus(2)
}

const batchDeleteChannels = async () => {
  if (!selectedChannels.value.length) return
  try {
    await ElMessageBox.confirm(
      `确认删除选中的 ${selectedChannels.value.length} 个渠道？此操作仅删除本地拉取的数据，不影响上游渠道。`,
      '批量删除',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  batchDeleting.value = true
  const ids = selectedChannels.value.map(ch => ch.channelId)
  try {
    const res = await axios.post('/api/channel-availability/delete', {
      channelIds: ids
    }, {
      headers: authHeaders()
    })
    channels.value = channels.value.filter(ch => !ids.includes(ch.channelId))
    selectedChannels.value = []
    ElMessage.success(res.data?.message || '删除成功')
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '删除失败')
  } finally {
    batchDeleting.value = false
  }
}

const batchUpdateStatus = async (status) => {
  const isEnable = status === 1
  if (isEnable) { batchEnabling.value = true } else { batchDisabling.value = true }
  const ids = selectedChannels.value.map(ch => ch.channelId)
  try {
    const res = await axios.post('/api/channel-availability/batch-status', {
      upstreamSiteId: selectedSiteId.value,
      channelIds: ids,
      status
    }, {
      headers: authHeaders()
    })
    const results = res.data?.results || []
    for (const r of results) {
      if (r.success) {
        channels.value = channels.value.map(ch =>
          ch.channelId === r.channelId ? { ...ch, status } : ch
        )
      }
    }
    ElMessage.success(res.data?.message || '操作完成')
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    batchEnabling.value = false
    batchDisabling.value = false
  }
}

const testSingleChannel = async (row) => {
  const id = row.channelId
  setChannelTesting(id, true)
  try {
    const res = await axios.post(`/api/channel-availability/test/${id}`, {
      testModel: ''
    }, {
      headers: authHeaders()
    })
    applyTestResult(id, res.data)
    if (res.data.success) {
      ElMessage.success(`渠道 ${id} 测试成功 (${res.data.responseTime}ms)`)
    } else {
      ElMessage.error(`渠道 ${id} 测试失败: ${res.data.error}`)
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.error || `渠道 ${id} 测试请求失败`)
  } finally {
    setChannelTesting(id, false)
  }
}

// --- 模型测试弹窗 ---

const openModelTestDialog = (row) => {
  modelTestChannelId.value = row.channelId
  modelTestChannelName.value = row.name || `渠道 ${row.channelId}`
  modelSearchText.value = ''
  selectedModels.value = []
  batchTestingModels.value = false

  const models = (row.models || '').split(',').map(m => m.trim()).filter(Boolean)
  modelList.value = models.map(m => ({
    model: m,
    status: 'pending',
    responseTime: 0,
    error: ''
  }))
  modelTestDialogVisible.value = true
}

const testOneModelInDialog = async (row) => {
  row.status = 'testing'
  row.error = ''
  row.responseTime = 0
  try {
    const res = await axios.post(
      `/api/channel-availability/test-models/${modelTestChannelId.value}`,
      { models: [row.model] },
      { headers: authHeaders() }
    )
    const result = (res.data?.results || [])[0]
    if (result) {
      row.status = result.success ? 'success' : 'failed'
      row.responseTime = result.responseTime || 0
      row.error = result.error || ''
    } else {
      row.status = 'failed'
      row.error = '无返回结果'
    }
  } catch (err) {
    row.status = 'failed'
    row.error = err.response?.data?.error || '请求失败'
  }
}

const batchTestModelsInDialog = async () => {
  const modelsToTest = selectedModels.value.map(m => m.model)
  if (!modelsToTest.length) return

  batchTestingModels.value = true
  for (const item of modelList.value) {
    if (modelsToTest.includes(item.model)) {
      item.status = 'testing'
      item.error = ''
      item.responseTime = 0
    }
  }

  try {
    const res = await axios.post(
      `/api/channel-availability/test-models/${modelTestChannelId.value}`,
      { models: modelsToTest },
      { headers: authHeaders() }
    )
    const resultsMap = {}
    for (const r of (res.data?.results || [])) {
      resultsMap[r.model] = r
    }
    for (const item of modelList.value) {
      const r = resultsMap[item.model]
      if (!r) continue
      item.status = r.success ? 'success' : 'failed'
      item.responseTime = r.responseTime || 0
      item.error = r.error || ''
    }
    const sc = res.data?.success_count || 0
    const fc = res.data?.fail_count || 0
    ElMessage.success(`测试完成：成功 ${sc}，失败 ${fc}`)
  } catch (err) {
    for (const item of modelList.value) {
      if (modelsToTest.includes(item.model) && item.status === 'testing') {
        item.status = 'failed'
        item.error = '请求失败'
      }
    }
    ElMessage.error(err.response?.data?.error || '批量模型测试失败')
  } finally {
    batchTestingModels.value = false
  }
}

const selectSuccessModels = () => {
  if (!modelTableRef.value) return
  modelList.value.forEach(item => {
    if (item.status === 'success') {
      modelTableRef.value.toggleRowSelection(item, true)
    }
  })
}

// --- 默认测试模型设置 ---

const formatCustomTestModels = (row) => {
  if (row.customTestModels && row.customTestModels.length) {
    return '默认测试模型: ' + row.customTestModels.join(', ')
  }
  return '设置默认测试模型'
}

const openCustomTestModelDialog = (row) => {
  customModelChannelId.value = row.channelId
  customModelChannelName.value = row.name || `渠道 ${row.channelId}`
  customModelSelection.value = row.customTestModels ? [...row.customTestModels] : []

  customModelOptions.value = (row.models || '').split(',').map(m => m.trim()).filter(Boolean)
  customModelDialogVisible.value = true
}

const saveCustomTestModels = async () => {
  savingCustomModels.value = true
  try {
    await axios.put(
      `/api/channel-availability/channels/${customModelChannelId.value}/test-models`,
      { models: customModelSelection.value },
      { headers: authHeaders() }
    )
    channels.value = channels.value.map(ch => {
      if (ch.channelId !== customModelChannelId.value) return ch
      return { ...ch, customTestModels: [...customModelSelection.value] }
    })
    ElMessage.success('默认测试模型已保存')
    customModelDialogVisible.value = false
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存失败')
  } finally {
    savingCustomModels.value = false
  }
}

// --- 推送配置 ---

const openNotifyConfigDialog = async () => {
  notifyConfigDialogVisible.value = true
  notifyConfigLoading.value = true
  notifyMissingIds.value = []
  notifyChannelSearch.value = ''
  notifyShowSelectedOnly.value = false
  try {
    const res = await axios.get('/api/channel-availability/notify-config', {
      headers: authHeaders(),
      params: { upstreamSiteId: selectedSiteId.value }
    })
    notifyForm.value = {
      enabled: res.data?.enabled || false,
      notificationType: res.data?.notificationType || 'feishu',
      webhookUrl: res.data?.webhookUrl || '',
      signKey: res.data?.signKey || '',
      weworkWebhookUrl: res.data?.weworkWebhookUrl || '',
      channelIds: res.data?.channelIds || [],
      statusFilter: res.data?.statusFilter ?? 0,
      refreshChannels: res.data?.refreshChannels ?? true,
      autoToggle: res.data?.autoToggle || false,
      slowThresholdMs: res.data?.slowThresholdMs ?? 0,
      schedules: (res.data?.schedules || []).map(s => ({
        startTime: s.start_time || '',
        endTime: s.end_time || '',
        intervalMinutes: s.interval_minutes || 30,
        startTimeObj: s.start_time || '',
        endTimeObj: s.end_time || ''
      }))
    }
    checkNotifyMissingIds()
  } catch {
    ElMessage.error('获取推送配置失败')
  } finally {
    notifyConfigLoading.value = false
  }
}

const filteredNotifyChannels = computed(() => {
  let list = channels.value
  if (notifyShowSelectedOnly.value) {
    const selected = new Set(notifyForm.value.channelIds)
    list = list.filter(ch => selected.has(ch.channelId))
  }
  const keyword = notifyChannelSearch.value.trim().toLowerCase()
  if (!keyword) return list
  return list.filter(ch =>
    String(ch.channelId).includes(keyword) ||
    (ch.name || '').toLowerCase().includes(keyword) ||
    (ch.group || '').toLowerCase().includes(keyword) ||
    (ch.tag || '').toLowerCase().includes(keyword)
  )
})

const selectAllFilteredNotifyChannels = () => {
  const current = new Set(notifyForm.value.channelIds)
  for (const ch of filteredNotifyChannels.value) {
    current.add(ch.channelId)
  }
  notifyForm.value.channelIds = [...current]
}

const selectAllNotifyChannels = () => {
  notifyForm.value.channelIds = channels.value.map(ch => ch.channelId)
}

const checkNotifyMissingIds = () => {
  const existingIds = new Set(channels.value.map(ch => ch.channelId))
  notifyMissingIds.value = (notifyForm.value.channelIds || []).filter(id => !existingIds.has(id))
}

const addNotifySchedule = () => {
  notifyForm.value.schedules.push({
    startTime: '09:00',
    endTime: '18:00',
    intervalMinutes: 30,
    startTimeObj: '09:00',
    endTimeObj: '18:00'
  })
}

const saveNotifyConfig = async () => {
  if (!notifyForm.value.channelIds.length) {
    ElMessage.warning('请配置至少一个生效渠道 ID')
    return
  }
  savingNotify.value = true
  const payload = {
    upstreamSiteId: selectedSiteId.value,
    ...notifyForm.value,
    schedules: (notifyForm.value.schedules || []).map(s => ({
      start_time: s.startTime || s.startTimeObj || '',
      end_time: s.endTime || s.endTimeObj || '',
      interval_minutes: s.intervalMinutes || 30
    }))
  }
  try {
    await axios.put('/api/channel-availability/notify-config', payload, {
      headers: authHeaders()
    })
    ElMessage.success('推送配置已保存')
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存推送配置失败')
  } finally {
    savingNotify.value = false
  }
}

const testNotifyPush = async () => {
  testingNotify.value = true
  try {
    const res = await axios.post('/api/channel-availability/notify-test', {
      upstreamSiteId: selectedSiteId.value
    }, {
      headers: authHeaders()
    })
    ElMessage.success(res.data?.message || '测试推送成功')
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '测试推送失败')
  } finally {
    testingNotify.value = false
  }
}

const runNotifyPush = async () => {
  if (!notifyForm.value.channelIds.length) {
    ElMessage.warning('请先配置生效渠道 ID 并保存')
    return
  }
  try {
    await ElMessageBox.confirm(
      '将拉取最新渠道信息、测试指定渠道、并推送测试成功的渠道通知，确认执行？',
      '执行推送',
      { confirmButtonText: '执行', cancelButtonText: '取消', type: 'info' }
    )
  } catch { return }

  runningNotify.value = true
  try {
    const res = await axios.post('/api/channel-availability/notify-run', {
      upstreamSiteId: selectedSiteId.value
    }, {
      headers: authHeaders()
    })
    const data = res.data || {}
    if (data.notified) {
      ElMessage.success(data.message + ' - 通知已推送')
    } else if (data.success === 0) {
      ElMessage.warning(data.message + ' - 无成功渠道，未推送')
    } else {
      ElMessage.info(data.message)
    }
    if (data.notifyError) {
      ElMessage.error('推送失败: ' + data.notifyError)
    }
    const missing = data.missingIds || []
    notifyMissingIds.value = missing

    if (data.results && data.results.length) {
      applyTestResults(data.results)
    }
    await loadChannels()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '执行推送失败')
  } finally {
    runningNotify.value = false
  }
}

// --- common ---

const applyTestResults = (results) => {
  const map = {}
  for (const r of results) {
    map[r.channelId] = r
  }
  channels.value = channels.value.map(ch => {
    const r = map[ch.channelId]
    if (!r) return ch
    return {
      ...ch,
      testResult: r.success ? 'success' : 'failed',
      responseTime: r.responseTime,
      testModel: r.testModel || ch.testModel,
      testError: r.error || '',
      testedAt: new Date().toISOString()
    }
  })
}

const applyTestResult = (channelId, result) => {
  channels.value = channels.value.map(ch => {
    if (ch.channelId !== channelId) return ch
    return {
      ...ch,
      testResult: result.success ? 'success' : 'failed',
      responseTime: result.responseTime,
      testModel: result.testModel || ch.testModel,
      testError: result.error || '',
      testedAt: new Date().toISOString()
    }
  })
}

const setChannelTesting = (channelId, value) => {
  const next = new Set(testingChannelIds.value)
  if (value) {
    next.add(channelId)
  } else {
    next.delete(channelId)
  }
  testingChannelIds.value = next
}

const loadSites = async () => {
  try {
    const res = await axios.get('/api/upstream-sites', { headers: authHeaders() })
    siteList.value = res.data || []
    if (siteList.value.length && !selectedSiteId.value) {
      selectedSiteId.value = siteList.value[0].id
    }
  } catch {
    // ignore
  }
}

const onSiteChange = () => {
  channels.value = []
  selectedChannels.value = []
  groups.value = []
  loadChannels()
  fetchGroups()
}

const logout = () => {
  localStorage.removeItem('token')
  window.location.href = '/login'
}

onMounted(async () => {
  await loadSites()
  if (selectedSiteId.value) {
    loadChannels()
    fetchGroups()
  }
})
</script>

<style scoped>
.channel-availability-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-left h2 {
  margin: 0;
  white-space: nowrap;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.table-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.filter-summary {
  color: #606266;
  font-size: 14px;
}

.models-text {
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}

.error-text {
  color: #f56c6c;
  font-size: 12px;
}

.config-alert {
  margin-bottom: 18px;
}

.model-test-alert {
  margin-bottom: 14px;
}

.model-test-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.model-test-summary {
  margin-top: 10px;
  color: #909399;
  font-size: 13px;
}

.form-tip {
  margin-top: 6px;
  color: #909399;
  font-size: 12px;
  line-height: 1.4;
}

.notify-missing-tags {
  margin-top: 8px;
}

.notify-channel-picker {
  width: 100%;
}

.notify-channel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}

.notify-channel-count {
  color: #909399;
  font-size: 12px;
  margin-left: auto;
}

.notify-channel-list {
  max-height: 240px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 8px 12px;
}

.notify-channel-list .el-checkbox-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.notify-channel-list .el-checkbox {
  margin-right: 0;
}

.notify-channel-empty {
  color: #909399;
  font-size: 13px;
  text-align: center;
  padding: 16px 0;
}

.schedules-wrapper {
  width: 100%;
}

.schedule-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}

.schedule-sep {
  color: #606266;
  font-size: 13px;
  flex-shrink: 0;
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
