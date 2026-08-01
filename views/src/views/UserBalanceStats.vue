<template>
  <div class="user-balance-container">
    <div class="header">
      <h2>各站点用户余额统计</h2>
      <div class="actions">
        <el-select v-model="selectedSiteId" placeholder="请选择上游站点" style="width: 220px">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-button type="primary" @click="fetchStats" :loading="loading" :disabled="!selectedSiteId">
          拉取并统计
        </el-button>
        <el-button
          type="warning"
          @click="fetchKeyCustomerBalance"
          :loading="loading"
          :disabled="!selectedSiteId"
        >
          查询重点客户余额
        </el-button>
        <el-button @click="exportCsv" :disabled="!stats || !stats.users.length">导出CSV</el-button>
        <el-button :icon="Setting" @click="settingsVisible = true" :disabled="!selectedSiteId">
          重点客户设置
        </el-button>
      </div>
    </div>

    <KeyCustomerSettingsDialog
      v-model:visible="settingsVisible"
      :site-id="selectedSiteId"
      @saved="onSettingsSaved"
    />

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <el-alert
      v-if="loading"
      type="info"
      :closable="false"
      title="正在从上游站点拉取全部用户数据，用户量较大时可能需要等待一段时间..."
      style="margin-bottom: 12px"
    />

    <el-alert
      v-if="stats && viewMode === 'key'"
      type="warning"
      :closable="false"
      title="当前显示的是重点客户余额信息"
      style="margin-bottom: 12px"
    />

    <div v-if="stats" class="summary-cards">
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ stats.totalUsers.toLocaleString() }}</div>
        <div class="card-label">用户总数</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ formatBalance(stats.totalBalance) }}</div>
        <div class="card-label">余额总计（¥）</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ formatBalance(stats.totalUsedBalance) }}</div>
        <div class="card-label">已消费总计（¥）</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ stats.positiveBalanceUsers.toLocaleString() }}</div>
        <div class="card-label">有余额用户数</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ (stats.elapsedMs / 1000).toFixed(1) }}s</div>
        <div class="card-label">统计耗时</div>
      </el-card>
    </div>

    <div v-if="stats" class="table-toolbar">
      <el-select v-model="balanceFilter" placeholder="余额筛选" style="width: 140px">
        <el-option label="全部余额" value="all" />
        <el-option label="有余额" value="positive" />
        <el-option label="零余额" value="zero" />
      </el-select>
      <el-input
        v-model="searchText"
        placeholder="搜索用户名/昵称/邮箱"
        clearable
        style="width: 240px"
      />
      <span class="filter-summary">显示 {{ filteredUsers.length }} / {{ stats.users.length }} 个用户</span>
    </div>

    <el-table
      v-if="stats"
      :data="filteredUsers"
      stripe
      border
      size="small"
      style="width: 100%"
      height="600"
      :row-class-name="warningRowClassName"
    >
      <el-table-column prop="id" label="ID" width="70" sortable />
      <el-table-column prop="username" label="用户名" min-width="140" show-overflow-tooltip />
      <el-table-column prop="displayName" label="昵称" min-width="140" show-overflow-tooltip />
      <el-table-column prop="group" label="分组" width="100" show-overflow-tooltip />
      <el-table-column label="余额（¥）" width="130" sortable prop="balance">
        <template #default="{ row }">
          <span :class="{ 'balance-positive': row.balance > 0 }">{{ formatBalance(row.balance) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="已消费（¥）" width="130" sortable prop="usedBalance">
        <template #default="{ row }">
          {{ formatBalance(row.usedBalance) }}
        </template>
      </el-table-column>
      <el-table-column prop="requestCount" label="请求数" width="90" sortable />
      <el-table-column label="邀请数" width="90" sortable prop="affCount" />
      <el-table-column label="邀请收益（¥）" width="130" sortable prop="affBalance">
        <template #default="{ row }">
          {{ formatBalance(row.affBalance) }}
        </template>
      </el-table-column>
      <el-table-column label="注册时间" width="170" sortable prop="createdAt">
        <template #default="{ row }">
          {{ formatDateTime(row.createdAt) }}
        </template>
      </el-table-column>
      <el-table-column label="最后登录" width="170" sortable prop="lastLoginAt">
        <template #default="{ row }">
          {{ formatDateTime(row.lastLoginAt) }}
        </template>
      </el-table-column>
      <el-table-column label="重点客户" width="110" fixed="right">
        <template #default="{ row }">
          <el-button
            size="small"
            :type="keyCustomerIds.has(row.id) ? 'danger' : 'default'"
            @click="toggleKeyCustomer(row)"
          >
            {{ keyCustomerIds.has(row.id) ? '取消标记' : '标记重点' }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="stats && stats.users.length === 0" description="暂无用户数据" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Setting } from '@element-plus/icons-vue'
import axios from 'axios'
import KeyCustomerSettingsDialog from '../components/KeyCustomerSettingsDialog.vue'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const selectedSiteId = ref('')
const loading = ref(false)
const errorMsg = ref('')
const stats = ref(null)
const balanceFilter = ref('all')
const searchText = ref('')
const viewMode = ref('all')
const keyCustomerIds = ref(new Set())
const warningThreshold = ref(0)
const settingsVisible = ref(false)

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

const loadKeyCustomerIds = async () => {
  if (!selectedSiteId.value) return
  try {
    const res = await axios.get('/api/key-customers', {
      params: { upstreamSiteId: selectedSiteId.value },
      headers: authHeaders()
    })
    keyCustomerIds.value = new Set(res.data?.userIds || [])
    warningThreshold.value = res.data?.warningThreshold || 0
  } catch {
    // ignore
  }
}

const onSettingsSaved = ({ warningThreshold: value }) => {
  warningThreshold.value = value
}

const warningRowClassName = ({ row }) => {
  if (warningThreshold.value > 0 && row.balance < warningThreshold.value) {
    return 'balance-warning-row'
  }
  return ''
}

const fetchStats = async () => {
  if (!selectedSiteId.value) return
  loading.value = true
  errorMsg.value = ''
  stats.value = null
  viewMode.value = 'all'
  try {
    const [statsRes] = await Promise.all([
      axios.get('/api/upstream-user-balance', {
        params: { upstreamSiteId: selectedSiteId.value },
        headers: authHeaders(),
        timeout: 600000
      }),
      loadKeyCustomerIds()
    ])
    stats.value = statsRes.data
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
  } finally {
    loading.value = false
  }
}

const fetchKeyCustomerBalance = async () => {
  if (!selectedSiteId.value) return
  loading.value = true
  errorMsg.value = ''
  stats.value = null
  viewMode.value = 'key'
  try {
    const [statsRes] = await Promise.all([
      axios.get('/api/key-customer-balance', {
        params: { upstreamSiteId: selectedSiteId.value },
        headers: authHeaders(),
        timeout: 600000
      }),
      loadKeyCustomerIds()
    ])
    stats.value = statsRes.data
    if (!stats.value.users.length) {
      ElMessage.info('该站点暂无标记的重点客户')
    }
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
  } finally {
    loading.value = false
  }
}

const toggleKeyCustomer = async (row) => {
  if (!selectedSiteId.value) return
  const marked = !keyCustomerIds.value.has(row.id)
  try {
    const res = await axios.post('/api/key-customers/toggle', {
      upstreamSiteId: selectedSiteId.value,
      userId: row.id,
      marked
    }, { headers: authHeaders() })
    keyCustomerIds.value = new Set(res.data?.userIds || [])
    ElMessage.success(marked ? '已标记为重点客户' : '已取消重点客户标记')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || e.message || '操作失败')
  }
}

const filteredUsers = computed(() => {
  if (!stats.value) return []
  const keyword = searchText.value.trim().toLowerCase()
  return stats.value.users.filter(user => {
    if (balanceFilter.value === 'positive' && !(user.balance > 0)) return false
    if (balanceFilter.value === 'zero' && user.balance > 0) return false
    if (keyword) {
      const haystack = `${user.username || ''} ${user.displayName || ''} ${user.email || ''}`.toLowerCase()
      if (!haystack.includes(keyword)) return false
    }
    return true
  })
})

const formatBalance = (value) => {
  if (value == null || Number.isNaN(value)) return '-'
  return value.toFixed(2)
}

const formatDateTime = (ts) => {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
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
  if (!stats.value || !stats.value.users.length) {
    ElMessage.warning('暂无数据可导出')
    return
  }

  const rows = [
    ['ID', '用户名', '昵称', '邮箱', '分组', '余额(¥)', '已消费(¥)', '请求数', '邀请数', '邀请收益(¥)', '注册时间', '最后登录'],
    ...stats.value.users.map(user => [
      user.id,
      user.username || '',
      user.displayName || '',
      user.email || '',
      user.group || '',
      user.balance.toFixed(2),
      user.usedBalance.toFixed(2),
      user.requestCount,
      user.affCount,
      user.affBalance.toFixed(2),
      formatDateTime(user.createdAt),
      formatDateTime(user.lastLoginAt)
    ])
  ]

  const csv = `﻿${rows.map(row => row.map(csvCell).join(',')).join('\r\n')}`
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const downloadUrl = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = downloadUrl
  link.download = `用户余额统计-${exportTimestamp()}.csv`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(downloadUrl)
}

onMounted(async () => {
  await loadSites()
})
</script>

<style scoped>
.user-balance-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 10px;
}

.header h2 {
  margin: 0;
  font-size: 18px;
}

.actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.summary-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 12px;
  margin-bottom: 20px;
}

.summary-card {
  text-align: center;
}

.summary-card :deep(.el-card__body) {
  padding: 16px;
}

.card-value {
  font-size: 24px;
  font-weight: 700;
  line-height: 1.4;
}

.card-label {
  font-size: 13px;
  color: #909399;
  margin-top: 4px;
}

.table-toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.filter-summary {
  font-size: 13px;
  color: #909399;
  margin-left: auto;
}

.balance-positive {
  color: #67c23a;
  font-weight: 600;
}

:deep(.balance-warning-row) {
  --el-table-tr-bg-color: #fef0f0;
}

:deep(.balance-warning-row td) {
  color: #f56c6c;
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
  .actions .el-select {
    width: 100% !important;
  }
  .table-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
  .filter-summary {
    margin-left: 0;
  }
  .summary-cards {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
