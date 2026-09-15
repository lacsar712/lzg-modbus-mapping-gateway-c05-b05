<template>
  <div class="card-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px;gap:12px;flex-wrap:wrap">
      <div>
        <h2 style="margin:0 0 4px;font-size:18px">映射配置</h2>
        <div class="sub" style="margin:0">编辑 YAML 后 reload；非法配置会失败并保留旧配置</div>
      </div>
      <div style="display:flex;gap:8px">
        <el-button :loading="loading" @click="load">重新加载文本</el-button>
        <el-button type="primary" :disabled="!auth.canWrite" :loading="reloading" @click="reload">热加载 Reload</el-button>
      </div>
    </div>
    <el-input
      v-model="yamlText"
      type="textarea"
      :rows="22"
      class="mono"
      :disabled="!auth.canWrite"
      placeholder="mapping YAML"
    />
    <el-alert
      v-if="lastMsg"
      :title="lastMsg"
      :type="lastOk ? 'success' : 'error'"
      show-icon
      style="margin-top:12px"
      :closable="false"
    />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api/client'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const yamlText = ref('')
const loading = ref(false)
const reloading = ref(false)
const lastMsg = ref('')
const lastOk = ref(true)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/mapping')
    yamlText.value = data.yaml || ''
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '加载失败')
  } finally {
    loading.value = false
  }
}

async function reload() {
  reloading.value = true
  lastMsg.value = ''
  try {
    const { data } = await api.post('/reload', { yaml: yamlText.value })
    lastOk.value = true
    lastMsg.value = `Reload 成功，设备数 ${data.devices}`
    if (data.yaml) yamlText.value = data.yaml
    ElMessage.success(lastMsg.value)
  } catch (e) {
    lastOk.value = false
    lastMsg.value = (e.response?.data?.error || 'Reload 失败') + (e.response?.data?.keptOld ? '（已保留旧配置）' : '')
    if (e.response?.data?.yaml) yamlText.value = e.response.data.yaml
    ElMessage.error(lastMsg.value)
  } finally {
    reloading.value = false
  }
}

onMounted(load)
</script>
