<template>
  <div class="bill-export-container">
    <div class="header">
      <h2>客户账单导出</h2>
      <div class="actions">
        <el-select v-model="queryForm.siteId" placeholder="请选择上游站点" style="width: 220px" @change="onSiteChange">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-button type="primary" @click="submitExport" :loading="submitting" :disabled="!queryForm.siteId">
          生成账单
        </el-button>
      </div>
    </div>

    <div class="filter-bar">
      <el-input v-model="queryForm.username" placeholder="用户名" clearable style="width: 160px" @keyup.enter="submitExport" />
      <el-input v-model="queryForm.userId" placeholder="用户ID" clearable style="width: 140px" @keyup.enter="submitExport" />
      <el-date-picker
        v-model="queryForm.timeRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        :shortcuts="timeShortcuts"
        value-format="X"
        style="width: 380px"
      />
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <div class="hint-bar">
      <el-icon><InfoFilled /></el-icon>
      <span>账单在后台生成，生成完成后可点击"下载"获取 CSV 文件，文件保留 24 小时后自动清理</span>
    </div>

    <el-table :data="jobs" stripe border size="small" style="width: 100%">
      <el-table-column label="用户" width="140">
        <template #default="{ row }">
          {{ row.username || row.userId }}
        </template>
      </el-table-column>
      <el-table-column label="时间范围" min-width="220">
        <template #default="{ row }">
          {{ formatTime(row.startTs) }} 至 {{ formatTime(row.endTs) }}
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="记录数" width="90">
        <template #default="{ row }">
          {{ row.status === 'completed' ? row.rowCount : '-' }}
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          {{ formatDateTime(row.createdAt) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.status === 'completed'" type="primary" link @click="downloadJob(row)">下载</el-button>
          <span v-else-if="row.status === 'failed'" class="text-error" :title="row.error">失败</span>
          <span v-else class="text-muted">-</span>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const submitting = ref(false)
const errorMsg = ref('')
const jobs = ref([])
const pollTimers = new Map()

const monthStart = (offset = 0) => {
  const d = new Date()
  d.setMonth(d.getMonth() + offset, 1)
  d.setHours(0, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}
const monthEnd = (offset = 0) => {
  const d = new Date()
  d.setMonth(d.getMonth() + offset + 1, 0)
  d.setHours(23, 59, 59, 0)
  return Math.floor(d.getTime() / 1000)
}
const nowTimestamp = () => Math.floor(Date.now() / 1000)

const queryForm = reactive({
  siteId: '',
  username: '',
  userId: '',
  timeRange: [String(monthStart()), String(nowTimestamp())]
})

const timeShortcuts = [
  { text: '本月', value: () => [new Date(monthStart() * 1000), new Date(monthEnd() * 1000)] },
  { text: '上月', value: () => [new Date(monthStart(-1) * 1000), new Date(monthEnd(-1) * 1000)] },
  { text: '最近7天', value: () => [new Date(Date.now() - 7 * 24 * 3600 * 1000), new Date()] },
  { text: '最近30天', value: () => [new Date(Date.now() - 30 * 24 * 3600 * 1000), new Date()] },
]

const loadSites = async () => {
  try {
    const res = await axios.get('/api/upstream-sites', { headers: authHeaders() })
    siteList.value = res.data || []
    if (siteList.value.length && !queryForm.siteId) {
      queryForm.siteId = siteList.value[0].id
    }
    if (queryForm.siteId) {
      await loadJobs()
    }
  } catch {
    // ignore
  }
}

const loadJobs = async () => {
  if (!queryForm.siteId) return
  try {
    const res = await axios.get('/api/customer-bill-export', {
      params: { upstreamSiteId: queryForm.siteId },
      headers: authHeaders()
    })
    jobs.value = res.data || []
    for (const job of jobs.value) {
      if (job.status === 'pending' || job.status === 'running') {
        pollJob(job.id)
      }
    }
  } catch {
    // ignore
  }
}

const onSiteChange = () => {
  jobs.value = []
  loadJobs()
}

const submitExport = async () => {
  if (!queryForm.siteId) {
    ElMessage.warning('请选择上游站点')
    return
  }
  if (!queryForm.username && !queryForm.userId) {
    ElMessage.warning('请输入用户名或用户ID')
    return
  }
  if (!queryForm.timeRange || queryForm.timeRange.length !== 2) {
    ElMessage.warning('请选择时间范围')
    return
  }

  submitting.value = true
  errorMsg.value = ''
  try {
    const payload = {
      upstreamSiteId: queryForm.siteId,
      startTs: Number(queryForm.timeRange[0]),
      endTs: Number(queryForm.timeRange[1])
    }
    if (queryForm.username) payload.username = queryForm.username
    if (queryForm.userId) payload.userId = queryForm.userId

    const res = await axios.post('/api/customer-bill-export', payload, { headers: authHeaders() })
    jobs.value.unshift(res.data)
    pollJob(res.data.id)
    ElMessage.success('账单生成任务已提交')
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '提交失败'
  } finally {
    submitting.value = false
  }
}

const pollJob = (jobId) => {
  if (pollTimers.has(jobId)) return
  const timer = setInterval(async () => {
    try {
      const res = await axios.get(`/api/customer-bill-export/${jobId}`, { headers: authHeaders() })
      const idx = jobs.value.findIndex(j => j.id === jobId)
      if (idx !== -1) jobs.value[idx] = res.data
      if (res.data.status === 'completed' || res.data.status === 'failed') {
        clearInterval(timer)
        pollTimers.delete(jobId)
      }
    } catch {
      clearInterval(timer)
      pollTimers.delete(jobId)
    }
  }, 3000)
  pollTimers.set(jobId, timer)
}

const downloadJob = (row) => {
  const url = `/api/customer-bill-export/${row.id}/download`
  axios.get(url, { headers: authHeaders(), responseType: 'blob' })
    .then((res) => {
      const blob = new Blob([res.data], { type: 'text/csv;charset=utf-8;' })
      const link = document.createElement('a')
      link.href = URL.createObjectURL(blob)
      link.download = row.fileName || `账单-${row.username || row.userId}.csv`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(link.href)
    })
    .catch((e) => {
      ElMessage.error(e.response?.data?.error || e.message || '下载失败')
    })
}

const formatTime = (ts) => {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const formatDateTime = (isoStr) => {
  if (!isoStr) return '-'
  const d = new Date(isoStr)
  return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

const statusLabel = (status) => {
  const map = { pending: '等待中', running: '生成中', completed: '已完成', failed: '失败' }
  return map[status] || status
}

const statusTag = (status) => {
  const map = { pending: 'info', running: 'warning', completed: 'success', failed: 'danger' }
  return map[status] || 'info'
}

onMounted(() => {
  loadSites()
})

onUnmounted(() => {
  for (const timer of pollTimers.values()) clearInterval(timer)
  pollTimers.clear()
})
</script>

<style scoped>
.bill-export-container {
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

.hint-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
  font-size: 13px;
  color: #909399;
}

.text-muted {
  color: #9ca3af;
}

.text-error {
  color: #f56c6c;
  cursor: help;
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
