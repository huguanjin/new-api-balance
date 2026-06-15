import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Balance from '../views/Balance.vue'
import MainLayout from '../views/MainLayout.vue'
import ModelDetection from '../views/ModelDetection.vue'
import ChannelAvailability from '../views/ChannelAvailability.vue'
import UpstreamSites from '../views/UpstreamSites.vue'
import CodexBalance from '../views/CodexBalance.vue'

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
