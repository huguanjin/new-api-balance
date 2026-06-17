<template>
  <div class="upstream-stats-container">
    <div class="header">
      <h2>分组成功率统计</h2>
      <div class="actions">
        <el-select v-model="selectedSiteId" placeholder="请选择上游站点" style="width: 220px" @change="onSiteChange">
          <el-option v-for="site in siteList" :key="site.id" :label="site.name" :value="site.id" />
        </el-select>
        <el-select
          v-model="selectedGroup"
          placeholder="全部分组"
          filterable
          clearable
          style="width: 200px"
          :disabled="!selectedSiteId"
        >
          <el-option label="全部分组" value="" />
          <el-option v-for="g in groupList" :key="g" :label="g" :value="g" />
        </el-select>
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
        <el-button type="primary" @click="fetchStats" :loading="loading" :disabled="!selectedSiteId">
          查询统计
        </el-button>
      </div>
    </div>

    <el-alert v-if="errorMsg" type="error" :closable="true" :title="errorMsg" @close="errorMsg = ''" style="margin-bottom: 12px" />

    <el-alert
      v-if="loading"
      type="info"
      :closable="false"
      title="正在从上游获取日志并聚合统计，数据量较大时可能需要等待数十秒..."
      style="margin-bottom: 12px"
    />

    <el-alert
      v-if="stats && stats.warnings && stats.warnings.length > 0"
      type="warning"
      :closable="true"
      :title="`部分数据拉取失败（${stats.warnings.length} 个错误），统计结果可能不完整`"
      style="margin-bottom: 12px"
    />

    <div v-if="stats" class="summary-cards">
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ stats.totalRecords.toLocaleString() }}</div>
        <div class="card-label">总请求数</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value" :style="{ color: rateColor(stats.overallRate) }">{{ stats.overallRate.toFixed(1) }}%</div>
        <div class="card-label">整体成功率</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ stats.successTotal.toLocaleString() }} / {{ stats.errorTotal.toLocaleString() }}</div>
        <div class="card-label">成功 / 错误</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ stats.groupCount }}</div>
        <div class="card-label">分组数量</div>
      </el-card>
      <el-card shadow="hover" class="summary-card">
        <div class="card-value">{{ (stats.elapsedMs / 1000).toFixed(1) }}s</div>
        <div class="card-label">统计耗时（{{ stats.fetchedPages }} 页）</div>
      </el-card>
    </div>

    <!-- Error Code Stats -->
    <div v-if="stats && stats.errorCodes && stats.errorCodes.length > 0" class="error-code-section">
      <h3>错误码分布</h3>
      <div class="error-code-cards">
        <div v-for="ec in stats.errorCodes" :key="ec.statusCode" class="error-code-card" :class="errorCodeClass(ec.statusCode)">
          <div class="ec-code">{{ ec.statusCode }}</div>
          <div class="ec-count">{{ ec.count.toLocaleString() }} 次</div>
          <div class="ec-percent">{{ ec.percent.toFixed(1) }}%</div>
        </div>
      </div>
    </div>

    <!-- Group Stats Table -->
    <el-table
      v-if="stats && stats.groups.length"
      :data="stats.groups"
      stripe
      border
      size="small"
      style="width: 100%"
      :default-sort="{ prop: 'successRate', order: 'ascending' }"
    >
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-models">
            <div class="expand-title">模型明细（{{ row.models.length }} 个模型）</div>
            <el-table :data="row.models" size="small" border stripe>
              <el-table-column prop="modelName" label="模型" min-width="180" show-overflow-tooltip />
              <el-table-column prop="totalCount" label="总数" width="80" sortable />
              <el-table-column prop="successCount" label="成功" width="80" />
              <el-table-column prop="errorCount" label="错误" width="80" />
              <el-table-column label="成功率" width="120" sortable :sort-method="(a, b) => a.successRate - b.successRate">
                <template #default="{ row: m }">
                  <el-tag :type="rateTagType(m.successRate)" size="small">
                    {{ m.successRate.toFixed(1) }}%
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="平均耗时" width="100" sortable :sort-method="(a, b) => a.avgUseTime - b.avgUseTime">
                <template #default="{ row: m }">
                  {{ (m.avgUseTime / 1000).toFixed(1) }}s
                </template>
              </el-table-column>
            </el-table>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="group" label="分组" min-width="180" show-overflow-tooltip />
      <el-table-column prop="totalCount" label="总请求" width="100" sortable />
      <el-table-column prop="successCount" label="成功" width="80" sortable />
      <el-table-column prop="errorCount" label="错误" width="80" sortable />
      <el-table-column label="成功率" width="200" sortable prop="successRate">
        <template #default="{ row }">
          <div class="rate-cell">
            <el-progress
              :percentage="row.successRate"
              :stroke-width="12"
              :color="rateColor(row.successRate)"
              :show-text="false"
              class="rate-progress"
            />
            <span class="rate-text" :style="{ color: rateColor(row.successRate) }">
              {{ row.successRate.toFixed(1) }}%
            </span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="平均耗时" width="100" sortable prop="avgUseTime">
        <template #default="{ row }">
          {{ (row.avgUseTime / 1000).toFixed(1) }}s
        </template>
      </el-table-column>
      <el-table-column label="总消耗" width="110" sortable prop="totalQuota">
        <template #default="{ row }">
          {{ formatQuota(row.totalQuota) }}
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="stats && stats.groups.length === 0" description="暂无统计数据" />
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import axios from 'axios'

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const siteList = ref([])
const selectedSiteId = ref('')
const selectedGroup = ref('')
const groupList = ref([])
const loading = ref(false)
const errorMsg = ref('')
const stats = ref(null)

const todayStart = () => {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}
const nowTimestamp = () => Math.floor(Date.now() / 1000)

const timeRange = ref([String(todayStart()), String(nowTimestamp())])

const timeShortcuts = [
  { text: '今天', value: () => [new Date(new Date().setHours(0, 0, 0, 0)), new Date()] },
  { text: '最近1小时', value: () => [new Date(Date.now() - 3600 * 1000), new Date()] },
  { text: '最近3小时', value: () => [new Date(Date.now() - 3 * 3600 * 1000), new Date()] },
  { text: '最近6小时', value: () => [new Date(Date.now() - 6 * 3600 * 1000), new Date()] },
  { text: '最近24小时', value: () => [new Date(Date.now() - 24 * 3600 * 1000), new Date()] },
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

const loadGroups = async () => {
  if (!selectedSiteId.value) {
    groupList.value = []
    return
  }
  try {
    const res = await axios.get('/api/upstream-groups', {
      params: { upstreamSiteId: selectedSiteId.value },
      headers: authHeaders()
    })
    const data = res.data?.data || res.data
    if (Array.isArray(data)) {
      groupList.value = data.map(g => typeof g === 'string' ? g : (g.name || g.key || String(g))).filter(Boolean)
    } else if (data && typeof data === 'object') {
      groupList.value = Object.keys(data)
    } else {
      groupList.value = []
    }
  } catch {
    groupList.value = []
  }
}

const onSiteChange = () => {
  stats.value = null
  selectedGroup.value = ''
  loadGroups()
}

watch(selectedSiteId, () => {
  if (selectedSiteId.value) loadGroups()
})

const fetchStats = async () => {
  if (!selectedSiteId.value) return
  if (!timeRange.value || timeRange.value.length !== 2) {
    errorMsg.value = '请选择时间范围'
    return
  }
  loading.value = true
  errorMsg.value = ''
  stats.value = null
  try {
    const params = {
      upstreamSiteId: selectedSiteId.value,
      start_timestamp: timeRange.value[0],
      end_timestamp: timeRange.value[1],
    }
    if (selectedGroup.value) {
      params.group = selectedGroup.value
    }
    const res = await axios.get('/api/upstream-log-stats', {
      params,
      headers: authHeaders(),
      timeout: 600000
    })
    stats.value = res.data
  } catch (e) {
    errorMsg.value = e.response?.data?.error || e.message || '查询失败'
  } finally {
    loading.value = false
  }
}

const rateColor = (rate) => {
  if (rate >= 99) return '#67c23a'
  if (rate >= 95) return '#e6a23c'
  return '#f56c6c'
}

const rateTagType = (rate) => {
  if (rate >= 99) return 'success'
  if (rate >= 95) return 'warning'
  return 'danger'
}

const formatQuota = (quota) => {
  if (quota == null) return '-'
  const val = quota / 500000
  if (val >= 1) return '$' + val.toFixed(2)
  return '$' + val.toFixed(4)
}

const errorCodeClass = (code) => {
  if (code === '429') return 'ec-rate-limit'
  if (code === '401' || code === '403') return 'ec-auth'
  if (code >= '500') return 'ec-server'
  return 'ec-client'
}

onMounted(async () => {
  await loadSites()
  if (selectedSiteId.value) {
    loadGroups()
  }
})
</script>

<style scoped>
.upstream-stats-container {
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

.rate-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rate-progress {
  flex: 1;
}

.rate-text {
  font-weight: 600;
  font-size: 13px;
  min-width: 50px;
  text-align: right;
}

.expand-models {
  padding: 12px 20px;
}

.expand-title {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 8px;
  color: #606266;
}

.error-code-section {
  margin-bottom: 20px;
}

.error-code-section h3 {
  font-size: 15px;
  margin: 0 0 12px 0;
  color: #303133;
}

.error-code-cards {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.error-code-card {
  padding: 12px 18px;
  border-radius: 8px;
  text-align: center;
  min-width: 100px;
  border: 1px solid #ebeef5;
}

.ec-code {
  font-size: 20px;
  font-weight: 700;
  line-height: 1.4;
}

.ec-count {
  font-size: 13px;
  color: #606266;
}

.ec-percent {
  font-size: 12px;
  color: #909399;
}

.ec-client {
  background: #fdf6ec;
  border-color: #e6a23c;
}
.ec-client .ec-code { color: #e6a23c; }

.ec-server {
  background: #fef0f0;
  border-color: #f56c6c;
}
.ec-server .ec-code { color: #f56c6c; }

.ec-auth {
  background: #f0f9eb;
  border-color: #67c23a;
}
.ec-auth .ec-code { color: #909399; }

.ec-rate-limit {
  background: #ecf5ff;
  border-color: #409eff;
}
.ec-rate-limit .ec-code { color: #409eff; }

@media (max-width: 768px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
  }
  .actions {
    flex-direction: column;
    width: 100%;
  }
  .actions .el-select,
  .actions .el-date-picker {
    width: 100% !important;
  }
  .summary-cards {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
