<template>
  <div class="upstream-sites">
    <div class="page-toolbar">
      <el-button type="primary" :icon="Plus" @click="openAddDialog">添加站点</el-button>
    </div>

    <el-table :data="sites" v-loading="loading" stripe style="width: 100%">
      <el-table-column prop="name" label="站点名称" min-width="140" />
      <el-table-column prop="url" label="API 地址" min-width="240" show-overflow-tooltip />
      <el-table-column label="Token" min-width="160">
        <template #default="{ row }">
          <span v-if="row.token">{{ maskToken(row.token) }}</span>
          <span v-else class="text-muted">未配置</span>
        </template>
      </el-table-column>
      <el-table-column prop="userId" label="UserID" width="100" />
      <el-table-column label="MySQL DSN" min-width="140">
        <template #default="{ row }">
          <span v-if="row.sqlDsn">{{ maskToken(row.sqlDsn) }}</span>
          <span v-else class="text-muted">未配置</span>
        </template>
      </el-table-column>
      <el-table-column label="状态码过滤" width="140">
        <template #default="{ row }">
          <span v-if="row.skipStatusCodes && row.skipStatusCodes.length">{{ row.skipStatusCodes.join(', ') }}</span>
          <span v-else class="text-muted">无</span>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          {{ formatDate(row.createdAt) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="160" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link @click="openEditDialog(row)">编辑</el-button>
          <el-button type="danger" link @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑站点' : '添加站点'" width="520px" :close-on-click-modal="false">
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="100px">
        <el-form-item label="站点名称" prop="name">
          <el-input v-model="form.name" placeholder="例如：主站点、备用站点" />
        </el-form-item>
        <el-form-item label="API 地址" prop="url">
          <el-input v-model="form.url" placeholder="https://example.com" />
        </el-form-item>
        <el-form-item label="Token" prop="token">
          <el-input v-model="form.token" placeholder="Bearer Token" show-password type="password" />
        </el-form-item>
        <el-form-item label="UserID">
          <el-input v-model="form.userId" placeholder="New-Api-User，默认为 1" />
        </el-form-item>
        <el-form-item label="状态码过滤">
          <el-select v-model="form.skipStatusCodes" multiple filterable allow-create default-first-option placeholder="输入状态码并回车，如 429" style="width: 100%">
          </el-select>
          <div class="form-hint">测试渠道时，包含这些状态码的错误将被视为通过</div>
        </el-form-item>
        <el-form-item label="MySQL DSN">
          <el-input v-model="form.sqlDsn" placeholder="user:password@tcp(host:3306)/dbname" show-password type="password" />
          <div class="form-hint">用于客户账单导出，直连该站点的 MySQL 数据库查询 logs 表</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import axios from 'axios'

const loading = ref(false)
const saving = ref(false)
const sites = ref([])
const dialogVisible = ref(false)
const isEdit = ref(false)
const editId = ref('')
const formRef = ref(null)

const form = reactive({
  name: '',
  url: '',
  token: '',
  userId: '',
  skipStatusCodes: [],
  sqlDsn: ''
})

const formRules = {
  name: [{ required: true, message: '请输入站点名称', trigger: 'blur' }],
  url: [{ required: true, message: '请输入 API 地址', trigger: 'blur' }]
}

const token = () => localStorage.getItem('token')
const authHeaders = () => ({ headers: { Authorization: 'Bearer ' + token() } })

const loadSites = async () => {
  loading.value = true
  try {
    const { data } = await axios.get('/api/upstream-sites', authHeaders())
    sites.value = data
  } catch {
    ElMessage.error('加载站点列表失败')
  } finally {
    loading.value = false
  }
}

const resetForm = () => {
  form.name = ''
  form.url = ''
  form.token = ''
  form.userId = ''
  form.skipStatusCodes = []
  form.sqlDsn = ''
  editId.value = ''
  isEdit.value = false
}

const openAddDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const openEditDialog = (row) => {
  isEdit.value = true
  editId.value = row.id
  form.name = row.name
  form.url = row.url
  form.token = row.token
  form.userId = row.userId || ''
  form.skipStatusCodes = (row.skipStatusCodes || []).map(String)
  form.sqlDsn = row.sqlDsn || ''
  dialogVisible.value = true
}

const handleSave = async () => {
  if (!formRef.value) return
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    saving.value = true
    try {
      const payload = {
        name: form.name,
        url: form.url,
        token: form.token,
        userId: form.userId,
        skipStatusCodes: form.skipStatusCodes.map(Number).filter(n => !isNaN(n) && n > 0),
        sqlDsn: form.sqlDsn
      }
      if (isEdit.value) {
        await axios.put(`/api/upstream-sites/${editId.value}`, payload, authHeaders())
        ElMessage.success('站点已更新')
      } else {
        await axios.post('/api/upstream-sites', payload, authHeaders())
        ElMessage.success('站点已创建')
      }
      dialogVisible.value = false
      await loadSites()
    } catch (e) {
      ElMessage.error(e.response?.data?.error || '保存失败')
    } finally {
      saving.value = false
    }
  })
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除站点"${row.name}"吗？关联的上游渠道和通知配置也会被删除。`,
      '删除确认',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'warning' }
    )
    await axios.delete(`/api/upstream-sites/${row.id}`, authHeaders())
    ElMessage.success('站点已删除')
    await loadSites()
  } catch (e) {
    if (e !== 'cancel') {
      ElMessage.error(e.response?.data?.error || '删除失败')
    }
  }
}

const maskToken = (t) => {
  if (!t || t.length <= 8) return '****'
  return t.substring(0, 4) + '****' + t.substring(t.length - 4)
}

const formatDate = (dateStr) => {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

onMounted(loadSites)
</script>

<style scoped>
.upstream-sites {
  max-width: 1200px;
}

.page-toolbar {
  margin-bottom: 16px;
  display: flex;
  justify-content: flex-end;
}

.text-muted {
  color: #9ca3af;
}

.form-hint {
  font-size: 12px;
  color: #9ca3af;
  margin-top: 4px;
  line-height: 1.4;
}
</style>
