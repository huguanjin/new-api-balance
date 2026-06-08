<template>
  <div class="login-container">
    <el-card class="login-card">
      <h2>New API Balance</h2>
      <el-form :model="loginForm" :rules="rules" ref="loginFormRef" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="loginForm.username" placeholder="Username" prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="loginForm.password" type="password" placeholder="Password" prefix-icon="Lock" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" native-type="submit" style="width: 100%" :loading="loading">Login</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const router = useRouter()
const route = useRoute()
const loginFormRef = ref(null)
const loading = ref(false)

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = {
  username: [{ required: true, message: 'Please input username', trigger: 'blur' }],
  password: [{ required: true, message: 'Please input password', trigger: 'blur' }]
}

const handleLogin = async () => {
  if (!loginFormRef.value) return
  await loginFormRef.value.validate(async (valid) => {
    if (valid) {
      loading.value = true
      try {
        const response = await axios.post('/api/login', loginForm)
        localStorage.setItem('token', response.data.token)
        ElMessage.success('Login successful')
        const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
          ? route.query.redirect
          : '/balance'
        router.push(redirect)
      } catch (error) {
        ElMessage.error(error.response?.data?.error || 'Login failed')
      } finally {
        loading.value = false
      }
    }
  })
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100vh;
  background-color: #f3f3f3;
}
.login-card {
  width: 400px;
  text-align: center;
}
</style>
