<template>
  <el-dialog
    :model-value="visible"
    title="重点客户设置"
    width="640px"
    @update:model-value="onVisibleChange"
    @open="onOpen"
  >
    <div class="settings-layout">
      <el-menu :default-active="activeCategory" class="settings-menu" @select="activeCategory = $event">
        <el-menu-item v-for="item in categories" :key="item.key" :index="item.key">
          {{ item.label }}
        </el-menu-item>
      </el-menu>

      <div class="settings-panel">
        <div v-if="activeCategory === 'warning'" class="panel-section">
          <h4>余额预警</h4>
          <p class="panel-hint">低于预警值的重点客户余额将在列表中标红提示，设为 0 表示关闭预警。</p>
          <el-form label-width="90px">
            <el-form-item label="预警值（¥）">
              <el-input-number
                v-model="warningThreshold"
                :min="0"
                :step="10"
                controls-position="right"
                style="width: 200px"
              />
            </el-form-item>
          </el-form>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button @click="onVisibleChange(false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const props = defineProps({
  visible: { type: Boolean, default: false },
  siteId: { type: String, default: '' }
})
const emit = defineEmits(['update:visible', 'saved'])

const authHeaders = () => ({
  Authorization: `Bearer ${localStorage.getItem('token')}`
})

const categories = [
  { key: 'warning', label: '余额预警' }
]
const activeCategory = ref('warning')
const warningThreshold = ref(0)
const saving = ref(false)

const onVisibleChange = (value) => {
  emit('update:visible', value)
}

const loadSettings = async () => {
  if (!props.siteId) return
  try {
    const res = await axios.get('/api/key-customers', {
      params: { upstreamSiteId: props.siteId },
      headers: authHeaders()
    })
    warningThreshold.value = res.data?.warningThreshold || 0
  } catch {
    // ignore
  }
}

const onOpen = () => {
  loadSettings()
}

watch(() => props.siteId, () => {
  if (props.visible) loadSettings()
})

const handleSave = async () => {
  if (!props.siteId) {
    ElMessage.warning('请先选择上游站点')
    return
  }
  saving.value = true
  try {
    await axios.put('/api/key-customers/warning-threshold', {
      upstreamSiteId: props.siteId,
      warningThreshold: warningThreshold.value || 0
    }, { headers: authHeaders() })
    ElMessage.success('设置已保存')
    emit('saved', { warningThreshold: warningThreshold.value || 0 })
    onVisibleChange(false)
  } catch (e) {
    ElMessage.error(e.response?.data?.error || e.message || '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.settings-layout {
  display: flex;
  gap: 16px;
  min-height: 260px;
}

.settings-menu {
  width: 160px;
  flex-shrink: 0;
  border-right: 1px solid var(--el-border-color-lighter);
}

.settings-panel {
  flex: 1;
  min-width: 0;
}

.panel-section h4 {
  margin: 0 0 8px;
  font-size: 15px;
}

.panel-hint {
  color: #909399;
  font-size: 13px;
  margin: 0 0 16px;
}
</style>
