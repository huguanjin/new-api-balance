<template>
  <div class="balance-container">
    <div class="header">
      <h2>余额管理系统</h2>
      <div>
        <el-button type="primary" @click="refreshAll" :loading="refreshing">刷新所有余额</el-button>
        <el-button type="success" @click="openAddSite">添加站点</el-button>
        <el-button type="danger" @click="logout">退出</el-button>
      </div>
    </div>

    <el-table :data="sites" style="width: 100%" border v-loading="loading">
      <el-table-column prop="name" label="站点名称" />
      <el-table-column prop="url" label="目标地址" />
      <el-table-column label="余额" width="200">
        <template #default="{ row }">
          <span v-if="row.balance !== undefined">
            <el-tag :type="row.balance > 0 ? 'success' : 'danger'">{{ row.balance.toFixed(4) }}</el-tag>
          </span>
          <span v-else-if="row.error" style="color: red">{{ row.error }}</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200">
        <template #default="{ row, $index }">
          <el-button size="small" @click="editSite($index)">编辑</el-button>
          <el-button size="small" type="danger" @click="deleteSite($index)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog :title="isEdit ? '编辑站点' : '添加站点'" v-model="dialogVisible" width="500px">
      <el-form :model="currentSite" label-width="100px">
        <el-form-item label="站点名称">
          <el-input v-model="currentSite.name" />
        </el-form-item>
        <el-form-item label="目标地址">
          <el-input v-model="currentSite.url" placeholder="如 https://api.xxx.com" />
        </el-form-item>
        <el-form-item label="Token">
          <el-input v-model="currentSite.token" placeholder="Bearer sk-xxxx" />
        </el-form-item>
        <el-form-item label="User ID (新)">
          <el-input v-model="currentSite.userId" placeholder="可选" />
        </el-form-item>
      </el-form>
      <template #footer>
        <span class="dialog-footer">
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button type="primary" @click="saveSite">保存</el-button>
        </span>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import axios from 'axios'

const router = useRouter()
const sites = ref([])
const loading = ref(false)
const refreshing = ref(false)

const dialogVisible = ref(false)
const isEdit = ref(false)
const editIndex = ref(-1)
const currentSite = ref({ name: '', url: '', token: '', userId: '' })

const fetchSites = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/sites', {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    })
    sites.value = res.data.map(site => ({ ...site, balance: undefined, error: '' }))
  } catch (err) {
    if (err.response?.status === 401) {
      logout()
    } else {
      ElMessage.error('无法获取站点列表')
    }
  } finally {
    loading.value = false
  }
}

const syncToServer = async () => {
  try {
    const listToSave = sites.value.map(s => ({
      name: s.name,
      url: s.url,
      token: s.token,
      userId: s.userId
    }))
    await axios.post('/api/sites', listToSave, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    })
    ElMessage.success('已保存配置')
  } catch (err) {
    ElMessage.error('保存失败')
  }
}

const refreshAll = async () => {
  refreshing.value = true
  const promises = sites.value.map(async (site) => {
    try {
      site.error = ''
      const targetUrl = `${normalizeSiteUrl(site.url)}/api/user/self`
      const res = await axios.get(`/api/proxy?url=${encodeURIComponent(targetUrl)}`, {
        headers: {
          'Authorization': `Bearer ${localStorage.getItem('token')}`,
          'Target-Authorization': site.token,
          'New-Api-User': site.userId || ''
        }
      })
      const data = res.data?.data || res.data
      if (data && data.quota !== undefined) {
        site.balance = quotaToUsd(data.quota)
      } else {
        site.error = '数据格式错误'
      }
    } catch (err) {
      site.error = '请求失败'
    }
  })
  await Promise.all(promises)
  refreshing.value = false
}

const normalizeSiteUrl = (value) => {
  const trimmed = (value || '').trim().replace(/\/+$/, '')
  if (!trimmed) return ''
  if (/^https?:\/\//i.test(trimmed)) return trimmed
  return `https://${trimmed}`
}

const quotaToUsd = (quota) => quota / 500000

const openAddSite = () => {
  isEdit.value = false
  currentSite.value = { name: '', url: '', token: '', userId: '' }
  dialogVisible.value = true
}

const editSite = (index) => {
  isEdit.value = true
  editIndex.value = index
  currentSite.value = { ...sites.value[index] }
  dialogVisible.value = true
}

const saveSite = () => {
  currentSite.value.url = normalizeSiteUrl(currentSite.value.url)
  if (isEdit.value) {
    sites.value[editIndex.value] = { ...sites.value[editIndex.value], ...currentSite.value }
  } else {
    sites.value.push({ ...currentSite.value })
  }
  dialogVisible.value = false
  syncToServer()
}

const deleteSite = (index) => {
  ElMessageBox.confirm('确认删除该站点吗?', '提示', { type: 'warning' }).then(() => {
    sites.value.splice(index, 1)
    syncToServer()
  }).catch(() => {})
}

const logout = () => {
  localStorage.removeItem('token')
  router.push('/login')
}

onMounted(() => {
  fetchSites()
})
</script>

<style scoped>
.balance-container {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
</style>
