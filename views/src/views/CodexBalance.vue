<template>
  <div class="codex-container">
    <div class="header">
      <h2>Codex 号池余额</h2>
      <div class="actions">
        <el-button type="primary" @click="queryBalance" :loading="querying" :disabled="!hasConfig">
          查询余额
        </el-button>
        <el-button @click="showConfigDialog = true">服务配置</el-button>
      </div>
    </div>

    <el-alert
      v-if="hasConfig && !querying && accounts.length === 0 && !queryError"
      type="info"
      :closable="false"
      title="点击「查询余额」获取 Codex 号池各账号的配额使用情况"
    />

    <el-alert
      v-if="queryError"
      type="error"
      :closable="true"
      :title="queryError"
      class="query-error"
      @close="queryError = ''"
    />

    <el-alert
      v-if="!hasConfig"
      type="warning"
      :closable="false"
      title="请先配置 Codex 服务信息后再查询余额"
    />

    <div v-if="accounts.length > 0" class="summary-bar">
      <span>共 {{ accounts.length }} 个账号，{{ successCount }} 个查询成功</span>
      <el-checkbox v-model="includeDisabled" @change="queryBalance">包含已禁用账号</el-checkbox>
    </div>

    <div v-if="accounts.length > 0" class="account-grid">
      <div
        v-for="(account, index) in accounts"
        :key="account.authIndex"
        class="account-card"
        :class="{ 'is-disabled': account.disabled, 'has-error': account.error }"
      >
        <div class="card-header">
          <div class="card-title">
            <span class="card-index">#{{ index + 1 }}</span>
            <span class="card-auth-index" :title="account.authIndex">{{ account.authIndex }}</span>
          </div>
          <div class="card-tags">
            <el-tag size="small" :type="account.disabled ? 'danger' : 'success'">
              {{ account.disabled ? '禁用' : '启用' }}
            </el-tag>
            <el-tag size="small" type="info" v-if="account.planType">
              {{ account.planType }}
            </el-tag>
            <el-tag size="small" :type="statusTagType(account.authStatus)">
              {{ account.authStatus || 'unknown' }}
            </el-tag>
          </div>
        </div>

        <div v-if="account.error" class="card-error">
          <el-icon><WarningFilled /></el-icon>
          <span>{{ account.error }}</span>
        </div>

        <div v-if="!account.error" class="card-quotas">
          <div class="quota-section" v-if="account.primary">
            <div class="quota-label">
              <span>5小时配额</span>
              <span class="quota-percent">{{ account.primary.usedPercent.toFixed(1) }}% 已用</span>
            </div>
            <el-progress
              :percentage="account.primary.usedPercent"
              :stroke-width="14"
              :color="progressColor(account.primary.usedPercent)"
              :show-text="false"
            />
            <div class="quota-detail">
              <span>剩余 {{ account.primary.remainingPercent.toFixed(1) }}%</span>
              <span>{{ formatWindowLabel(account.primary.limitWindowSeconds) }}</span>
              <span>重置于 {{ account.primary.resetAt }}（{{ formatDuration(account.primary.resetAfterSeconds) }}后）</span>
            </div>
          </div>
          <div class="quota-section" v-else>
            <div class="quota-label"><span>5小时配额</span></div>
            <div class="quota-unavailable">数据不可用</div>
          </div>

          <div class="quota-section" v-if="account.secondary">
            <div class="quota-label">
              <span>周配额</span>
              <span class="quota-percent">{{ account.secondary.usedPercent.toFixed(1) }}% 已用</span>
            </div>
            <el-progress
              :percentage="account.secondary.usedPercent"
              :stroke-width="14"
              :color="progressColor(account.secondary.usedPercent)"
              :show-text="false"
            />
            <div class="quota-detail">
              <span>剩余 {{ account.secondary.remainingPercent.toFixed(1) }}%</span>
              <span>{{ formatWindowLabel(account.secondary.limitWindowSeconds) }}</span>
              <span>重置于 {{ account.secondary.resetAt }}（{{ formatDuration(account.secondary.resetAfterSeconds) }}后）</span>
            </div>
          </div>
          <div class="quota-section" v-else>
            <div class="quota-label"><span>周配额</span></div>
            <div class="quota-unavailable">数据不可用</div>
          </div>
        </div>
      </div>
    </div>

    <el-dialog v-model="showConfigDialog" title="Codex 服务配置" width="500px" :close-on-click-modal="false">
      <el-form :model="configForm" label-width="120px" v-loading="configLoading">
        <el-form-item label="服务地址">
          <el-input v-model="configForm.baseUrl" placeholder="如 http://34.70.48.141:8317" />
        </el-form-item>
        <el-form-item label="管理员 Token">
          <el-input v-model="configForm.adminToken" type="password" show-password placeholder="管理API的Bearer Token" />
        </el-form-item>
        <el-form-item label="超时时间">
          <el-input-number v-model="configForm.timeout" :min="5" :max="120" :step="5" />
          <span class="form-suffix">秒</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showConfigDialog = false">取消</el-button>
        <el-button type="primary" @click="saveConfig" :loading="savingConfig">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { WarningFilled } from '@element-plus/icons-vue'
import axios from 'axios'

const accounts = ref([])
const querying = ref(false)
const queryError = ref('')
const includeDisabled = ref(false)
const showConfigDialog = ref(false)
const configLoading = ref(false)
const savingConfig = ref(false)
const configForm = ref({ baseUrl: '', adminToken: '', timeout: 30 })
const hasConfig = ref(false)

const successCount = computed(() => accounts.value.filter(a => !a.error).length)

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const fetchConfig = async () => {
  try {
    const res = await axios.get('/api/codex/config', { headers: authHeaders() })
    configForm.value = {
      baseUrl: res.data?.baseUrl || '',
      adminToken: res.data?.adminToken || '',
      timeout: res.data?.timeout || 30
    }
    hasConfig.value = !!configForm.value.baseUrl
  } catch {
    hasConfig.value = false
  }
}

const saveConfig = async () => {
  if (!configForm.value.baseUrl.trim()) {
    ElMessage.error('请填写服务地址')
    return
  }
  savingConfig.value = true
  try {
    await axios.put('/api/codex/config', configForm.value, { headers: authHeaders() })
    ElMessage.success('配置已保存')
    hasConfig.value = true
    showConfigDialog.value = false
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '保存失败')
  } finally {
    savingConfig.value = false
  }
}

const queryBalance = async () => {
  querying.value = true
  queryError.value = ''
  try {
    const res = await axios.post('/api/codex/query', {
      includeDisabled: includeDisabled.value
    }, { headers: authHeaders() })
    accounts.value = res.data?.accounts || []
  } catch (err) {
    queryError.value = err.response?.data?.error || '查询失败'
    accounts.value = []
  } finally {
    querying.value = false
  }
}

const progressColor = (percent) => {
  if (percent >= 90) return '#f56c6c'
  if (percent >= 70) return '#e6a23c'
  return '#67c23a'
}

const statusTagType = (status) => {
  if (status === 'active' || status === 'ok') return 'success'
  if (status === 'error' || status === 'banned') return 'danger'
  return 'warning'
}

const formatWindowLabel = (seconds) => {
  if (!seconds || seconds <= 0) return ''
  const hours = seconds / 3600
  if (Math.abs(hours - Math.round(hours)) < 0.01) {
    return `${Math.round(hours)}h 窗口`
  }
  return `${seconds}s 窗口`
}

const formatDuration = (seconds) => {
  if (!seconds || seconds <= 0) return '即将'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const parts = []
  if (days) parts.push(`${days}天`)
  if (hours) parts.push(`${hours}小时`)
  if (minutes || !parts.length) parts.push(`${minutes}分钟`)
  return parts.join(' ')
}

onMounted(() => {
  fetchConfig()
})
</script>

<style scoped>
.codex-container {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}

.actions {
  display: flex;
  gap: 8px;
}

.query-error {
  margin-bottom: 16px;
}

.summary-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding: 10px 16px;
  background: #f0f2f5;
  border-radius: 6px;
  font-size: 14px;
  color: #606266;
}

.account-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(420px, 1fr));
  gap: 16px;
}

.account-card {
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  padding: 18px;
  transition: box-shadow 0.2s;
}

.account-card:hover {
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
}

.account-card.is-disabled {
  opacity: 0.7;
}

.account-card.has-error {
  border-color: #f56c6c;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 14px;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.card-index {
  color: #909399;
  font-size: 13px;
  flex-shrink: 0;
}

.card-auth-index {
  font-weight: 600;
  font-size: 14px;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-tags {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.card-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 10px 12px;
  background: #fef0f0;
  border-radius: 6px;
  color: #f56c6c;
  font-size: 13px;
  line-height: 1.5;
  word-break: break-all;
}

.card-quotas {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.quota-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.quota-label {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  color: #606266;
  font-weight: 500;
}

.quota-percent {
  color: #909399;
}

.quota-detail {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 12px;
  color: #909399;
}

.quota-unavailable {
  color: #c0c4cc;
  font-size: 13px;
  font-style: italic;
}

.form-suffix {
  margin-left: 8px;
  color: #606266;
}

@media (max-width: 768px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
  }

  .account-grid {
    grid-template-columns: 1fr;
  }
}
</style>
