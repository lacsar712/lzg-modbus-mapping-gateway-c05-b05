<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="brand">Modbus 点位监控台</div>
      <div class="sub">登录后查看设备 snapshot / 写点 / 映射 reload</div>
      <el-form @submit.prevent="onSubmit" label-position="top">
        <el-form-item label="账号">
          <el-input v-model="username" placeholder="engineer / observer" autocomplete="username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="password" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-button type="primary" style="width:100%" :loading="loading" native-type="submit">登录</el-button>
      </el-form>
      <p class="sub" style="margin-top:16px;margin-bottom:0">
        engineer/mod123456（可写） · observer/obs123456（只读）
      </p>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const username = ref('engineer')
const password = ref('mod123456')
const loading = ref(false)

async function onSubmit() {
  loading.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    ElMessage.success('登录成功')
    router.push({ name: 'devices' })
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>
