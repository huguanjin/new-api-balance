import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Balance from '../views/Balance.vue'
import MainLayout from '../views/MainLayout.vue'
import ModelDetection from '../views/ModelDetection.vue'
import ChannelAvailability from '../views/ChannelAvailability.vue'
import UpstreamSites from '../views/UpstreamSites.vue'
import CodexBalance from '../views/CodexBalance.vue'
import UpstreamLogs from '../views/UpstreamLogs.vue'
import UpstreamStats from '../views/UpstreamStats.vue'
import UserBalanceStats from '../views/UserBalanceStats.vue'
import Dashboard from '../views/Dashboard.vue'
import BillExport from '../views/BillExport.vue'
import CustomSqlExport from '../views/CustomSqlExport.vue'

const routes = [
  {
    path: '/',
    redirect: '/balance'
  },
  {
    path: '/login',
    name: 'Login',
    component: Login
  },
  {
    path: '/',
    component: MainLayout,
    meta: { requiresAuth: true },
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: Dashboard
      },
      {
        path: 'balance',
        name: 'Balance',
        component: Balance
      },
      {
        path: 'model-detection',
        name: 'ModelDetection',
        component: ModelDetection
      },
      {
        path: 'channel-availability',
        name: 'ChannelAvailability',
        component: ChannelAvailability
      },
      {
        path: 'upstream-sites',
        name: 'UpstreamSites',
        component: UpstreamSites
      },
      {
        path: 'codex-balance',
        name: 'CodexBalance',
        component: CodexBalance
      },
      {
        path: 'upstream-logs',
        name: 'UpstreamLogs',
        component: UpstreamLogs
      },
      {
        path: 'upstream-stats',
        name: 'UpstreamStats',
        component: UpstreamStats
      },
      {
        path: 'user-balance-stats',
        name: 'UserBalanceStats',
        component: UserBalanceStats
      },
      {
        path: 'bill-export',
        name: 'BillExport',
        component: BillExport
      },
      {
        path: 'custom-sql-export',
        name: 'CustomSqlExport',
        component: CustomSqlExport
      }
    ]
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  if (to.meta.requiresAuth && !token) {
    next({ path: '/login', query: { redirect: to.fullPath } })
  } else if (to.path === '/login' && token) {
    const redirect = typeof to.query.redirect === 'string' && to.query.redirect.startsWith('/')
      ? to.query.redirect
      : '/balance'
    next(redirect)
  } else {
    next()
  }
})

export default router
