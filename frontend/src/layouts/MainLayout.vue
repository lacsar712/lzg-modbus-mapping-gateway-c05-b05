<template>
  <div>
    <header class="layout-header">
      <div>
        <div class="brand" style="font-size:18px;margin:0">Modbus 点位监控台</div>
        <div class="sub" style="margin:2px 0 0">工业寄存器映射网关</div>
      </div>
      <nav class="nav">
        <router-link to="/devices">设备列表</router-link>
        <router-link to="/mapping">映射配置</router-link>
        <router-link to="/diagnostics">连接诊断</router-link>
      </nav>
      <div style="display:flex;align-items:center;gap:10px;color:var(--muted);font-size:13px">
        <span>{{ auth.username }} ({{ auth.role }})</span>
        <el-button size="small" @click="onLogout">退出</el-button>
      </div>
    </header>
    <main class="page">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { useAuthStore } from '../stores/auth'
import { useRouter } from 'vue-router'

const auth = useAuthStore()
const router = useRouter()
function onLogout() {
  auth.logout()
  router.push({ name: 'login' })
}
</script>
