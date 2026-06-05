import { createRouter, createWebHistory } from 'vue-router'
import Login from '../views/Login.vue'
import Balance from '../views/Balance.vue'

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
    path: '/balance',
    name: 'Balance',
    component: Balance,
    meta: { requiresAuth: true }
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  if (to.meta.requiresAuth && !token) {
    next('/login')
  } else if (to.path === '/login' && token) {
    next('/balance')
  } else {
    next()
  }
})

export default router
