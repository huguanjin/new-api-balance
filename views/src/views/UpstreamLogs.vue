<template>
  <div class="upstream-logs-container">
    <div class="header">
      <h2>上游日志查询</h2>
      <div class="actions">
        <el-select v-model="selectedSiteId" placeholder="请选择上游站点" style="width: 220px" @change="onSiteChange">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-button type="primary" @click="queryLogs" :loading="loading" :disabled="!selectedSiteId">
          查询
        </el-button>
      </div>
    </div>

    <div class="filter-bar">
      <el-date-picker
        v-model="timeRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :shortcuts="timeShortcuts"
        value-format="X"
        style="width: 380px"
      />
      <el-input v-model="filters.model_name" placeholder="模型名称" clearable style="width: 160px" @keyup.enter="queryLogs" />
      <el-input v-model="filters.username" placeholder="用户名" clearable style="width: 130px" @keyup.enter="queryLogs" />
      <el-input v-model="filters.token_name" placeholder="令牌名称" clearable style="width: 130px" @keyup.enter="queryLogs" />
      <el-input v-model="filters.channel" placeholder="渠道ID" clearable style="width: 100px" @keyup.enter="queryLogs" />
      <el-select v-model="filters.type" placeholder="日志类型" clearable style="width: 120px">
        <el-option label="全部" value="" />
        <el-option label="消费" :value="2" />
        <el-option label="错误" :value="5" />
      </el-select>
      <el-input v-model="filters.content" placeholder="内容搜索" clearable style="width: 160px" @keyup.enter="queryLogs" />
      <el-input v-model="filters.keyword" placeholder="关键词" clearable style="width: 140px" @keyup.enter="queryLogs" />
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <el-table
      :data="logs"
      v-loading="loading"
      stripe
      border
      size="small"
      style="width: 100%"
      :row-class-name="logRowClass"
      @sort-change="handleSortChange"
    >
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-detail">
            <div v-if="row.prompt_tokens || row.completion_tokens">
              <strong>Token 用量：</strong>
              Prompt: {{ row.prompt_tokens }} / Completion: {{ row.completion_tokens }}
            </div>
            <div v-if="row.request_id">
              <strong>请求ID：</strong>{{ row.request_id }}
            </div>
            <div v-if="row.group">
              <strong>分组：</strong>{{ row.group }}
            </div>
            <div v-if="row.other" class="other-info">
              <strong>其他信息：</strong>
              <pre class="log-content">{{ formatOther(row.other) }}</pre>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="时间" width="170" sortable="custom">
        <template #default="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column prop="username" label="用户" width="100" show-overflow-tooltip />
      <el-table-column prop="token_name" label="令牌" width="120" show-overflow-tooltip />
      <el-table-column prop="model_name" label="模型" min-width="150" show-overflow-tooltip />
      <el-table-column prop="channel" label="渠道" width="70" />
      <el-table-column prop="channel_name" label="渠道名称" width="140" show-overflow-tooltip />
      <el-table-column prop="type" label="类型" width="70">
        <template #default="{ row }">
          <el-tag size="small" :type="logTypeTag(row.type)">{{ logTypeLabel(row.type) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="content" label="内容" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <span :class="{ 'error-content': row.type === 5 }">{{ row.content || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="quota" label="消耗" width="90">
        <template #default="{ row }">
          {{ row.quota != null ? formatQuota(row.quota) : '-' }}
        </template>
      </el-table-column>
      <el-table-column prop="use_time" label="耗时" width="80">
        <template #default="{ row }">
          {{ row.use_time != null ? (row.use_time / 1000).toFixed(1) + 's' : '-' }}
        </template>
      </el-table-column>
      <el-table-column prop="is_stream" label="流式" width="55">
        <template #default="{ row }">
          {{ row.is_stream ? '是' : '否' }}
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination-bar" v-if="total > 0">
      <span class="total-info">共 {{ total }} 条记录</span>
      <el-pagination
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next, jumper"
        @current-change="queryLogs"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const selectedSiteId = ref('')
const loading = ref(false)
const errorMsg = ref('')
const logs = ref([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = 100

const todayStart = () => {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}
const nowTimestamp = () => Math.floor(Date.now() / 1000)

const timeRange = ref([String(todayStart()), String(nowTimestamp())])

const filters = reactive({
  model_name: '',
  username: '',
  token_name: '',
  channel: '',
  content: '',
  keyword: '',
  type: ''
})

const timeShortcuts = [
  { text: '今天', value: () => [new Date(new Date().setHours(0, 0, 0, 0)), new Date()] },
  { text: '最近1小时', value: () => [new Date(Date.now() - 3600 * 1000), new Date()] },
  { text: '最近3小时', value: () => [new Date(Date.now() - 3 * 3600 * 1000), new Date()] },
  { text: '最近24小时', value: () => [new Date(Date.now() - 24 * 3600 * 1000), new Date()] },
  { text: '最近7天', value: () => [new Date(Date.now() - 7 * 24 * 3600 * 1000), new Date()] },
]

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
  logs.value = []
  total.value = 0
  currentPage.value = 1
  queryLogs()
}

const queryLogs = async () => {
  if (!selectedSiteId.value) return
  loading.value = true
  errorMsg.value = ''
  try {
    const params = {
      upstreamSiteId: selectedSiteId.value,
      p: currentPage.value,
      page_size: pageSize
    }
    if (timeRange.value && timeRange.value.length === 2) {
      params.start_timestamp = timeRange.value[0]
      params.end_timestamp = timeRange.value[1]
    }
    for (const key of ['model_name', 'username', 'token_name', 'channel', 'content', 'keyword', 'type']) {
      if (filters[key] !== '' && filters[key] != null) {
        params[key] = filters[key]
      }
    }
    const res = await axios.get('/api/upstream-logs', { params, headers: authHeaders() })
    const body = res.data
    let items = []
    let count = 0
    if (body && body.data && typeof body.data === 'object' && !Array.isArray(body.data)) {
      items = body.data.items || body.data.data || []
      count = body.data.total_count || body.data.total || items.length
    } else if (body && Array.isArray(body.data)) {
      items = body.data
      count = body.total_count || body.total || items.length
    } else if (Array.isArray(body)) {
      items = body
      count = items.length
    }
    logs.value = items
    total.value = count
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
    logs.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

const handleSortChange = () => {
  queryLogs()
}

const formatTime = (ts) => {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const formatQuota = (quota) => {
  if (quota == null) return '-'
  const val = quota / 500000
  if (val >= 1) return '$' + val.toFixed(2)
  return '$' + val.toFixed(4)
}

const logTypeLabel = (type) => {
  const map = { 2: '消费', 5: '错误' }
  return map[type] || String(type)
}

const logTypeTag = (type) => {
  const map = { 2: '', 5: 'danger' }
  return map[type] || 'info'
}

const logRowClass = ({ row }) => {
  if (row.type === 5) return 'row-error'
  return ''
}

const formatOther = (other) => {
  if (!other) return ''
  try {
    return JSON.stringify(JSON.parse(other), null, 2)
  } catch {
    return other
  }
}

onMounted(async () => {
  await loadSites()
  if (selectedSiteId.value) {
    queryLogs()
  }
})
</script>

<style scoped>
.upstream-logs-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header h2 {
  margin: 0;
  font-size: 18px;
}

.actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  align-items: center;
}

.pagination-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 16px;
}

.total-info {
  font-size: 13px;
  color: #909399;
}

.expand-detail {
  padding: 12px 20px;
  font-size: 13px;
  line-height: 1.8;
}

.log-content {
  margin: 4px 0;
  padding: 8px 12px;
  background: #f5f7fa;
  border-radius: 4px;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
  max-height: 300px;
  overflow: auto;
}

.other-info {
  margin-top: 8px;
}

:deep(.row-error) {
  background-color: #fef0f0 !important;
}

.error-content {
  color: #f56c6c;
}

@media (max-width: 768px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .filter-bar {
    flex-direction: column;
  }
  .filter-bar .el-input,
  .filter-bar .el-select,
  .filter-bar .el-date-picker {
    width: 100% !important;
  }
}
</style>
