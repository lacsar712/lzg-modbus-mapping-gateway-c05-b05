<template>
  <div class="card-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px">
      <div>
        <h2 style="margin:0 0 4px;font-size:18px">连接诊断</h2>
        <div class="sub" style="margin:0">健康检查与最近一次 Modbus 错误</div>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-descriptions :column="1" border>
      <el-descriptions-item label="Status">{{ health.status || '-' }}</el-descriptions-item>
      <el-descriptions-item label="Mapping Loaded">{{ health.mappingLoaded ? '是' : '否' }}</el-descriptions-item>
      <el-descriptions-item label="Device Count">{{ health.deviceCount ?? '-' }}</el-descriptions-item>
      <el-descriptions-item label="Last Modbus Error">
        <span class="mono">{{ health.lastModbusError || '无' }}</span>
      </el-descriptions-item>
      <el-descriptions-item label="Last Error At">{{ health.lastErrorAt || '-' }}</el-descriptions-item>
    </el-descriptions>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api/client'

const health = ref({})
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/health')
    health.value = data
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '健康检查失败')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
