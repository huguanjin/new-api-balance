<template>
  <div class="bill-export-container">
    <div class="header">
      <h2>客户账单导出</h2>
      <div class="actions">
        <el-select v-model="queryForm.siteId" placeholder="请选择上游站点" style="width: 220px">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-button type="primary" @click="queryBill" :loading="loading" :disabled="!queryForm.siteId">
          查询
        </el-button>
        <el-button type="success" @click="exportCsv" :loading="exporting" :disabled="!items.length">
          导出CSV
        </el-button>
      </div>
    </div>

    <div class="filter-bar">
      <el-input v-model="queryForm.username" placeholder="用户名" clearable style="width: 160px" @keyup.enter="queryBill" />
      <el-input v-model="queryForm.userId" placeholder="用户ID" clearable style="width: 140px" @keyup.enter="queryBill" />
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

    <div class="summary-bar" v-if="items.length">
      <span>共 {{ items.length }} 条记录</span>
      <span>总费用：${{ totalCost.toFixed(4) }}</span>
    </div>

    <el-table :data="items" v-loading="loading" stripe border size="small" style="width: 100%">
      <el-table-column prop="id" label="记录ID" width="110" />
      <el-table-column prop="user_id" label="用户ID" width="90" />
      <el-table-column prop="username" label="用户名" width="120" show-overflow-tooltip />
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column prop="model_name" label="模型名称" min-width="160" show-overflow-tooltip />
      <el-table-column prop="group" label="分组" width="130" show-overflow-tooltip />
      <el-table-column prop="prompt_tokens" label="输入" width="90" />
      <el-table-column prop="completion_tokens" label="输出" width="90" />
      <el-table-column label="费用(USD)" width="100">
        <template #default="{ row }">
          {{ formatQuota(row.quota) }}
        </template>
      </el-table-column>
      <el-table-column prop="is_stream" label="流式" width="60">
        <template #default="{ row }">
          {{ row.is_stream ? '是' : '否' }}
        </template>
      </el-table-column>
      <el-table-column prop="request_id" label="请求ID" min-width="200" show-overflow-tooltip />
    </el-table>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const loading = ref(false)
const exporting = ref(false)
const errorMsg = ref('')
const items = ref([])

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

const totalCost = computed(() => items.value.reduce((sum, row) => sum + (row.quota || 0) / 500000, 0))

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

const queryBill = async () => {
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

  loading.value = true
  errorMsg.value = ''
  try {
    const params = {
      upstreamSiteId: queryForm.siteId,
      start_timestamp: queryForm.timeRange[0],
      end_timestamp: queryForm.timeRange[1]
    }
    if (queryForm.username) params.username = queryForm.username
    if (queryForm.userId) params.user_id = queryForm.userId

    const res = await axios.get('/api/customer-bill-export', {
      params,
      headers: authHeaders(),
      timeout: 120000
    })
    items.value = res.data.items || []
    if (!items.value.length) {
      ElMessage.warning('没有符合条件的记录')
    }
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
    items.value = []
  } finally {
    loading.value = false
  }
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

const csvCell = (value) => {
  const text = String(value ?? '')
  return `"${text.replace(/"/g, '""')}"`
}

const exportTimestamp = () => {
  const date = new Date()
  const pad = (v) => String(v).padStart(2, '0')
  return [date.getFullYear(), pad(date.getMonth() + 1), pad(date.getDate())].join('') +
    '-' + [pad(date.getHours()), pad(date.getMinutes()), pad(date.getSeconds())].join('')
}

const exportCsv = () => {
  if (!items.value.length) return
  exporting.value = true
  try {
    const rows = [
      ['记录ID', '用户ID', '用户名', '创建时间', '模型名称', '分组', '输入', '输出', '费用(USD)', '流式', '请求ID'],
      ...items.value.map(row => [
        row.id,
        row.user_id,
        row.username || '',
        formatTime(row.created_at),
        row.model_name || '',
        row.group || '',
        row.prompt_tokens || '',
        row.completion_tokens || '',
        row.quota != null ? (row.quota / 500000).toFixed(4) : '',
        row.is_stream ? '是' : '否',
        row.request_id || ''
      ])
    ]

    const csv = `﻿${rows.map(r => r.map(csvCell).join(',')).join('\r\n')}`
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    const nameHint = queryForm.username || queryForm.userId
    link.download = `账单-${nameHint}-${exportTimestamp()}.csv`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)

    ElMessage.success(`导出完成，共 ${items.value.length} 条记录`)
  } finally {
    exporting.value = false
  }
}

onMounted(loadSites)
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

.summary-bar {
  display: flex;
  gap: 24px;
  margin-bottom: 12px;
  font-size: 13px;
  color: #606266;
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
