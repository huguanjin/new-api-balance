<template>
  <div class="settlement-container">
    <div class="header">
      <h2>查询结算金额</h2>
    </div>

    <div class="filter-bar">
      <el-select v-model="queryForm.siteId" placeholder="请选择上游站点" style="width: 220px">
        <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
      </el-select>
      <el-input
        v-model="queryForm.identity"
        placeholder="用户名或用户ID"
        clearable
        style="width: 220px"
        @keyup.enter="runQuery"
      />
      <el-date-picker
        v-model="queryForm.timeRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :shortcuts="timeShortcuts"
        value-format="YYYY-MM-DD HH:mm:ss"
        style="width: 380px"
      />
      <el-button type="primary" :icon="Search" :loading="loading" @click="runQuery">查询</el-button>
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <el-alert
      v-if="!queryForm.siteId && !siteList.length"
      type="warning"
      :closable="false"
      title="暂无上游站点，请先在「上游站点」中配置站点及其 MySQL DSN"
      style="margin-bottom: 12px"
    />

    <template v-if="result">
      <el-card class="result-card" shadow="never">
        <div class="result-amount">
          <div class="result-label">消费额度</div>
          <div class="result-value">$ {{ formatAmount(result.amount) }}</div>
          <div class="result-sub">
            quota 合计 {{ result.quotaSum.toLocaleString() }} （1 USD = 500,000 quota）
          </div>
        </div>
        <el-descriptions :column="2" border size="small" class="result-meta">
          <el-descriptions-item label="上游站点">{{ result.siteName || '-' }}</el-descriptions-item>
          <el-descriptions-item label="查询对象">{{ result.identity }}</el-descriptions-item>
          <el-descriptions-item label="时间范围">
            {{ result.startAt }} 至 {{ result.endAt }}
          </el-descriptions-item>
          <el-descriptions-item label="消耗记录数">{{ result.rowCount }}</el-descriptions-item>
          <el-descriptions-item label="Unix 时间戳" :span="2">
            {{ result.startTs }} ~ {{ result.endTs }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <div class="sql-block">
        <div class="sql-title">实际执行的 SQL</div>
        <pre>{{ result.sql }}</pre>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const loading = ref(false)
const errorMsg = ref('')
const result = ref(null)

const pad = (n) => String(n).padStart(2, '0')
const formatDateTime = (d) =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`

// Shortcuts hand the picker local Date objects; value-format turns them into
// "YYYY-MM-DD HH:mm:ss" strings that the backend reads as Beijing time.
const monthStart = (offset = 0) => {
  const d = new Date()
  d.setMonth(d.getMonth() + offset, 1)
  d.setHours(0, 0, 0, 0)
  return d
}
const monthEnd = (offset = 0) => {
  const d = new Date()
  d.setMonth(d.getMonth() + offset + 1, 0)
  d.setHours(23, 59, 59, 0)
  return d
}

const timeShortcuts = [
  { text: '今日', value: () => { const s = new Date(); s.setHours(0, 0, 0, 0); const e = new Date(); e.setHours(23, 59, 59, 0); return [s, e] } },
  { text: '本月', value: () => [monthStart(), monthEnd()] },
  { text: '上月', value: () => [monthStart(-1), monthEnd(-1)] },
  { text: '最近7天', value: () => [new Date(Date.now() - 6 * 24 * 3600 * 1000), new Date()] },
  { text: '最近30天', value: () => [new Date(Date.now() - 29 * 24 * 3600 * 1000), new Date()] }
]

const queryForm = reactive({
  siteId: '',
  identity: '',
  timeRange: [formatDateTime(monthStart()), formatDateTime(monthEnd())]
})

const loadSites = async () => {
  try {
    const res = await axios.get('/api/upstream-sites', { headers: authHeaders() })
    siteList.value = res.data || []
    if (siteList.value.length && !queryForm.siteId) {
      queryForm.siteId = siteList.value[0].id
    }
  } catch {
    // ignore
  }
}

const formatAmount = (value) => {
  if (typeof value !== 'number' || Number.isNaN(value)) return '-'
  return value.toFixed(6)
}

const runQuery = async () => {
  if (!queryForm.siteId) {
    ElMessage.warning('请选择上游站点')
    return
  }
  if (!queryForm.identity || !queryForm.identity.trim()) {
    ElMessage.warning('请输入用户名或用户ID')
    return
  }
  if (!queryForm.timeRange || queryForm.timeRange.length !== 2) {
    ElMessage.warning('请选择时间范围')
    return
  }

  loading.value = true
  errorMsg.value = ''
  try {
    const res = await axios.post('/api/settlement/query', {
      upstreamSiteId: queryForm.siteId,
      identity: queryForm.identity.trim(),
      startTime: queryForm.timeRange[0],
      endTime: queryForm.timeRange[1]
    }, { headers: authHeaders() })
    result.value = res.data
  } catch (e) {
    result.value = null
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadSites()
})
</script>

<style scoped>
.settlement-container {
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

.filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  align-items: center;
}

.result-card {
  margin-bottom: 16px;
  border-radius: 10px;
}

.result-amount {
  padding-bottom: 16px;
  border-bottom: 1px solid #eef2f7;
  margin-bottom: 16px;
}

.result-label {
  color: #6b7280;
  font-size: 13px;
}

.result-value {
  margin-top: 6px;
  color: #2563eb;
  font-size: 32px;
  font-weight: 700;
  line-height: 1.2;
}

.result-sub {
  margin-top: 6px;
  color: #9ca3af;
  font-size: 12px;
}

.result-meta :deep(.el-descriptions__label) {
  width: 110px;
}

.sql-block {
  background: #0f172a;
  border-radius: 10px;
  padding: 14px 16px;
  overflow-x: auto;
}

.sql-title {
  color: #94a3b8;
  font-size: 12px;
  margin-bottom: 8px;
}

.sql-block pre {
  margin: 0;
  color: #e2e8f0;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
