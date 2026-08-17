<template>
  <div class="custom-sql-export-container">
    <div class="header">
      <h2>自定义查询导出</h2>
      <div class="actions">
        <el-select v-model="queryForm.siteId" placeholder="请选择上游站点" style="width: 220px" @change="onSiteChange">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-button type="primary" @click="submitExport" :loading="submitting" :disabled="!queryForm.siteId">
          执行并导出
        </el-button>
      </div>
    </div>

    <div class="filter-bar">
      <el-input
        v-model="queryForm.sql"
        type="textarea"
        :rows="6"
        placeholder="仅支持单条 SELECT 查询语句，例如：SELECT id, username, quota FROM users LIMIT 100"
        style="width: 100%"
      />
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <div class="hint-bar">
      <el-icon><InfoFilled /></el-icon>
      <span>仅支持只读 SELECT 查询，结果最多导出 {{ maxRows.toLocaleString() }} 行；查询在后台执行，完成后可下载 CSV 或压缩后下载，文件保留 24 小时后自动清理</span>
    </div>

    <el-table :data="jobs" stripe border size="small" style="width: 100%">
      <el-table-column label="SQL" min-width="260">
        <template #default="{ row }">
          <span class="sql-cell" :title="row.sql">{{ row.sql }}</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="记录数" width="120">
        <template #default="{ row }">
          <template v-if="row.status === 'completed'">
            {{ row.rowCount }}<span v-if="row.truncated" class="text-warning">（已截断）</span>
          </template>
          <template v-else>-</template>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          {{ formatDateTime(row.createdAt) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <template v-if="row.status === 'completed'">
            <el-button type="primary" link @click="downloadJob(row, 'csv')">下载 CSV</el-button>
            <el-button type="primary" link @click="downloadJob(row, 'zip')">压缩下载</el-button>
          </template>
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

const maxRows = 500000

const siteList = ref([])
const submitting = ref(false)
const errorMsg = ref('')
const jobs = ref([])
const pollTimers = new Map()

const queryForm = reactive({
  siteId: '',
  sql: ''
})

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
    const res = await axios.get('/api/custom-sql-export', {
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
  if (!queryForm.sql.trim()) {
    ElMessage.warning('请输入 SQL 查询语句')
    return
  }

  submitting.value = true
  errorMsg.value = ''
  try {
    const payload = {
      upstreamSiteId: queryForm.siteId,
      sql: queryForm.sql
    }

    const res = await axios.post('/api/custom-sql-export', payload, { headers: authHeaders() })
    jobs.value.unshift(res.data)
    pollJob(res.data.id)
    ElMessage.success('查询导出任务已提交')
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
      const res = await axios.get(`/api/custom-sql-export/${jobId}`, { headers: authHeaders() })
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

const downloadJob = (row, format) => {
  const url = `/api/custom-sql-export/${row.id}/download${format === 'zip' ? '?format=zip' : ''}`
  axios.get(url, { headers: authHeaders(), responseType: 'blob' })
    .then((res) => {
      const isZip = format === 'zip'
      const blob = new Blob([res.data], { type: isZip ? 'application/zip' : 'text/csv;charset=utf-8;' })
      const defaultName = isZip
        ? (row.fileName || '自定义查询.csv').replace(/\.csv$/i, '.zip')
        : (row.fileName || '自定义查询.csv')
      const link = document.createElement('a')
      link.href = URL.createObjectURL(blob)
      link.download = defaultName
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(link.href)
    })
    .catch((e) => {
      ElMessage.error(e.response?.data?.error || e.message || '下载失败')
    })
}

const formatDateTime = (isoStr) => {
  if (!isoStr) return '-'
  const d = new Date(isoStr)
  return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

const statusLabel = (status) => {
  const map = { pending: '等待中', running: '查询中', completed: '已完成', failed: '失败' }
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
.custom-sql-export-container {
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

.sql-cell {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  font-family: monospace;
  font-size: 12px;
}

.text-muted {
  color: #9ca3af;
}

.text-error {
  color: #f56c6c;
  cursor: help;
}

.text-warning {
  color: #e6a23c;
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
  .filter-bar .el-select {
    width: 100% !important;
  }
}
</style>
