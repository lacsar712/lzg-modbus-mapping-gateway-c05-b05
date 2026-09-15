<template>
  <div class="card-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px;gap:12px;flex-wrap:wrap">
      <div>
        <h2 style="margin:0 0 4px;font-size:18px">点位监控 · {{ deviceId }}</h2>
        <div class="sub" style="margin:0">Snapshot 定时刷新；可写点位可提交写值</div>
      </div>
      <div style="display:flex;gap:8px;align-items:center">
        <el-switch v-model="autoRefresh" active-text="自动刷新" />
        <el-button :loading="loading" @click="load">刷新</el-button>
        <el-button @click="$router.push('/devices')">返回</el-button>
      </div>
    </div>

    <el-table :data="points" v-loading="loading" empty-text="无点位">
      <el-table-column prop="name" label="点位" min-width="120" />
      <el-table-column prop="address" label="地址" width="80" />
      <el-table-column prop="type" label="类型" min-width="120" />
      <el-table-column label="可写" width="70">
        <template #default="{ row }">
          <el-tag :type="row.writable ? 'success' : 'info'" size="small">{{ row.writable ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="值" min-width="120">
        <template #default="{ row }">
          <span class="mono">{{ formatVal(row.value) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="Raw" min-width="120">
        <template #default="{ row }">
          <span class="mono">{{ (row.raw || []).join(',') }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="quality" label="质量" width="90" />
      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <el-button
            v-if="row.writable"
            type="primary"
            link
            :disabled="!auth.canWrite"
            @click="openWrite(row)"
          >写值</el-button>
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="writeVisible" title="写点位" width="420px" destroy-on-close>
      <el-form label-position="top" @submit.prevent="submitWrite">
        <el-form-item label="点位">
          <el-input :model-value="current?.name" disabled />
        </el-form-item>
        <el-form-item label="工程值">
          <el-input-number v-model="writeValue" :controls="true" style="width:100%" />
        </el-form-item>
        <p class="sub">将按 scale/offset 逆运算并校验 min/max 后写回 Holding Registers。</p>
      </el-form>
      <template #footer>
        <el-button @click="writeVisible = false">取消</el-button>
        <el-button type="primary" :loading="writing" @click="submitWrite">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api/client'
import { useAuthStore } from '../stores/auth'

const route = useRoute()
const auth = useAuthStore()
const deviceId = computed(() => route.params.id)
const points = ref([])
const loading = ref(false)
const autoRefresh = ref(true)
const writeVisible = ref(false)
const current = ref(null)
const writeValue = ref(0)
const writing = ref(false)
let timer = null

function formatVal(v) {
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(4)
  return String(v ?? '')
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get(`/devices/${deviceId.value}/snapshot`)
    points.value = data.points || []
  } catch (e) {
    ElMessage.error(e.response?.data?.error || 'snapshot 失败')
  } finally {
    loading.value = false
  }
}

function openWrite(row) {
  current.value = row
  writeValue.value = typeof row.value === 'boolean' ? (row.value ? 1 : 0) : Number(row.value) || 0
  writeVisible.value = true
}

async function submitWrite() {
  writing.value = true
  try {
    await api.put(`/devices/${deviceId.value}/points/${current.value.name}`, { value: writeValue.value })
    ElMessage.success('写值成功')
    writeVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '写值失败')
  } finally {
    writing.value = false
  }
}

function setupTimer() {
  if (timer) clearInterval(timer)
  timer = null
  if (autoRefresh.value) {
    timer = setInterval(load, 3000)
  }
}

watch(autoRefresh, setupTimer)
onMounted(() => {
  load()
  setupTimer()
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>
