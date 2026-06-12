<template>
  <el-container class="app-shell">
    <el-aside class="app-sidebar" width="220px">
      <div class="brand">
        <div class="brand-mark">N</div>
        <div>
          <div class="brand-title">New API Balance</div>
          <div class="brand-subtitle">渠道与模型管理</div>
        </div>
      </div>
      <el-menu
        class="side-menu"
        :default-active="route.path"
        router
      >
        <el-menu-item index="/balance">
          <el-icon><Coin /></el-icon>
          <span>余额管理</span>
        </el-menu-item>
        <el-menu-item index="/model-detection">
          <el-icon><Monitor /></el-icon>
          <span>模型检测</span>
        </el-menu-item>
        <el-menu-item index="/channel-availability">
          <el-icon><Connection /></el-icon>
          <span>渠道可用性</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="app-header">
        <div class="mobile-nav">
          <el-button
            :type="route.path === '/balance' ? 'primary' : 'default'"
            :icon="Coin"
            @click="router.push('/balance')"
          >
            余额
          </el-button>
          <el-button
            :type="route.path === '/model-detection' ? 'primary' : 'default'"
            :icon="Monitor"
            @click="router.push('/model-detection')"
          >
            模型检测
          </el-button>
          <el-button
            :type="route.path === '/channel-availability' ? 'primary' : 'default'"
            :icon="Connection"
            @click="router.push('/channel-availability')"
          >
            渠道可用性
          </el-button>
        </div>
        <div class="header-title">
          <h1>{{ pageTitle }}</h1>
          <span>{{ pageDescription }}</span>
        </div>
        <el-button type="danger" plain :icon="SwitchButton" @click="logout">退出</el-button>
      </el-header>

      <el-main class="app-main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Coin, Monitor, Connection, SwitchButton } from '@element-plus/icons-vue'

const route = useRoute()
const router = useRouter()

const pageMeta = {
  '/model-detection': { title: '模型真实性检测', description: '配置检测模型、提交 Veridrop 任务并跟踪报告' },
  '/channel-availability': { title: '上游渠道可用性检测', description: '获取上游渠道列表，批量检测渠道可用性' },
}

const pageTitle = computed(() => pageMeta[route.path]?.title || '余额管理')

const pageDescription = computed(() => pageMeta[route.path]?.description || '维护渠道、查询余额、导入上游渠道并发送余额通知')

const logout = () => {
  localStorage.removeItem('token')
  router.push('/login')
}
</script>

<style scoped>
.app-shell {
  min-height: 100vh;
  background: #f5f7fb;
  color: #1f2937;
}

.app-sidebar {
  display: flex;
  flex-direction: column;
  background: #111827;
  border-right: 1px solid #111827;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 20px 18px;
  color: #ffffff;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  background: #2563eb;
  font-weight: 700;
}

.brand-title {
  font-size: 15px;
  font-weight: 700;
  line-height: 1.2;
}

.brand-subtitle {
  margin-top: 3px;
  color: #9ca3af;
  font-size: 12px;
  line-height: 1.2;
}

.side-menu {
  flex: 1;
  border-right: 0;
  background: transparent;
}

.side-menu :deep(.el-menu-item) {
  color: #cbd5e1;
}

.side-menu :deep(.el-menu-item.is-active),
.side-menu :deep(.el-menu-item:hover) {
  color: #ffffff;
  background: #1f2937;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  height: 72px;
  padding: 0 24px;
  background: #ffffff;
  border-bottom: 1px solid #e5e7eb;
}

.header-title {
  min-width: 0;
  text-align: left;
}

.header-title h1 {
  margin: 0;
  color: #111827;
  font-size: 20px;
  font-weight: 700;
  line-height: 1.3;
  letter-spacing: 0;
}

.header-title span {
  display: block;
  margin-top: 3px;
  color: #6b7280;
  font-size: 13px;
  line-height: 1.4;
}

.app-main {
  padding: 20px 24px 28px;
  overflow-x: hidden;
}

.mobile-nav {
  display: none;
  gap: 8px;
}

@media (max-width: 900px) {
  .app-shell {
    display: block;
  }

  .app-sidebar {
    display: none;
  }

  .app-header {
    height: auto;
    flex-wrap: wrap;
    padding: 14px 16px;
  }

  .mobile-nav {
    display: flex;
    order: 2;
    width: 100%;
  }

  .mobile-nav .el-button {
    flex: 1;
  }

  .header-title {
    flex: 1;
  }

  .app-main {
    padding: 14px;
  }
}
</style>
