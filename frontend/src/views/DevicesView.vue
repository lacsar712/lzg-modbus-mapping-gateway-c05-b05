<template>
  <div class="card-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px">
      <div>
        <h2 style="margin:0 0 4px;font-size:18px">设备列表</h2>
        <div class="sub" style="margin:0">选择设备进入点位监控</div>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </div>
    <el-table :data="devices" v-loading="loading" empty-text="暂无设备">
      <el-table-column prop="id" label="设备 ID" min-width="140" />
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column prop="endpoint" label="Endpoint" min-width="160" />
      <el-table-column prop="unitId" label="Unit" width="80" />
      <el-table-column prop="timeoutMs" label="超时(ms)" width="100" />
      <el-table-column label="点位数" width="90">
        <template #default="{ row }">{{ row.points?.length || 0 }}</template>
      </el-table-column>
      <el-table-column label="操作" width="120">
        <template #default="{ row }">
          <el-button type="primary" link @click="$router.push(`/devices/${row.id}/monitor`)">监控</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api/client'

const devices = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/devices')
    devices.value = data.devices || []
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
