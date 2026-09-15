<template>
  <div class="card-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px;gap:12px;flex-wrap:wrap">
      <div>
        <h2 style="margin:0 0 4px;font-size:18px">连接诊断</h2>
        <div class="sub" style="margin:0">
          采样状态机 + 持久化采样环（{{ overview.ringSize ?? 0 }}/{{ overview.ringCap ?? 0 }} 条）
        </div>
      </div>
      <div style="display:flex;gap:8px;align-items:center">
        <el-switch v-model="autoRefresh" active-text="自动刷新" />
        <el-button :loading="loading" @click="loadAll">刷新</el-button>
      </div>
    </div>

    <el-alert
      v-if="overview.loadError"
      type="warning"
      :closable="false"
      style="margin-bottom:12px"
      :title="`诊断状态文件加载失败（已从空状态启动）：${overview.loadError}`"
    />

    <!-- 设备矩阵 -->
    <div style="display:flex;justify-content:space-between;align-items:center;margin:6px 0 10px">
      <h3 style="margin:0;font-size:15px">设备采样矩阵</h3>
      <el-radio-group v-model="sortBy" size="small" @change="loadDevices">
        <el-radio-button value="">默认顺序</el-radio-button>
        <el-radio-button value="lastFailure">最近失败优先</el-radio-button>
        <el-radio-button value="avgDuration">平均耗时优先</el-radio-button>
      </el-radio-group>
    </div>

    <el-table :data="devices" v-loading="loading" empty-text="暂无设备">
      <el-table-column prop="id" label="设备" min-width="120" />
      <el-table-column label="采样状态" width="110">
        <template #default="{ row }">
          <el-tag :type="stateTag(row.diag?.state)" size="small">{{ stateLabel(row.diag?.state) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="周期(ms)" width="90">
        <template #default="{ row }">{{ row.diag?.intervalMs || '-' }}</template>
      </el-table-column>
      <el-table-column label="最近结果" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.diag?.stats?.lastSuccess === true" type="success" size="small">成功</el-tag>
          <el-tag v-else-if="row.diag?.stats?.lastSuccess === false" type="danger" size="small">失败</el-tag>
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
      <el-table-column label="平均耗时" width="100">
        <template #default="{ row }">
          <span class="mono">{{ row.diag?.stats?.total ? row.diag.stats.avgDurationMs + ' ms' : '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="成功/失败" width="100">
        <template #default="{ row }">
          <span class="mono">{{ row.diag?.stats?.successCount ?? 0 }}/{{ row.diag?.stats?.failCount ?? 0 }}</span>
        </template>
      </el-table-column>
      <el-table-column label="坏点" width="70">
        <template #default="{ row }">{{ row.diag?.stats?.badPointTotal ?? 0 }}</template>
      </el-table-column>
      <el-table-column label="最近错误" min-width="200">
        <template #default="{ row }">
          <span v-if="row.diag?.stats?.lastError" class="mono" style="color:var(--danger,#f56c6c)">
            {{ row.diag.stats.lastError }}
          </span>
          <span v-else class="sub">无</span>
        </template>
      </el-table-column>
      <el-table-column v-if="auth.canWrite" label="操作" width="150" fixed="right">
        <template #default="{ row }">
          <el-button
            v-if="!row.diag || row.diag.state === 'idle'"
            type="primary" link @click="openStart(row.id)"
          >启动采样</el-button>
          <el-button
            v-else-if="row.diag.state === 'running'"
            type="danger" link @click="stopSampler(row.id)"
          >停止</el-button>
          <span v-else class="sub">停止中…</span>
        </template>
      </el-table-column>
    </el-table>

    <!-- 采样记录环 -->
    <div style="display:flex;justify-content:space-between;align-items:center;margin:22px 0 10px">
      <h3 style="margin:0;font-size:15px">采样记录（环形缓冲，最新在前）</h3>
      <el-select v-model="sampleFilter" size="small" style="width:160px" @change="loadSamples">
        <el-option label="全部设备" value="" />
        <el-option v-for="d in devices" :key="d.id" :label="d.id" :value="d.id" />
      </el-select>
    </div>

    <el-table :data="samples" v-loading="loading" empty-text="暂无采样记录" max-height="420">
      <el-table-column prop="seq" label="#" width="70" />
      <el-table-column label="时间" min-width="170">
        <template #default="{ row }"><span class="mono">{{ fmtTime(row.startedAt) }}</span></template>
      </el-table-column>
      <el-table-column prop="deviceId" label="设备" min-width="110" />
      <el-table-column label="来源" width="90">
        <template #default="{ row }">
          <el-tag :type="row.source === 'manual' ? 'info' : 'warning'" size="small">
            {{ row.source === 'manual' ? '手动' : '采样器' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="耗时" width="90">
        <template #default="{ row }"><span class="mono">{{ row.durationMs }} ms</span></template>
      </el-table-column>
      <el-table-column label="结果" width="80">
        <template #default="{ row }">
          <el-tag :type="row.success ? 'success' : 'danger'" size="small">
            {{ row.success ? '成功' : '失败' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="坏点" min-width="130">
        <template #default="{ row }">
          <span v-if="row.badPoints?.length" class="mono">{{ row.badPoints.join(', ') }}</span>
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
      <el-table-column label="错误" min-width="200">
        <template #default="{ row }">
          <span v-if="row.error" class="mono" style="color:var(--danger,#f56c6c)">{{ row.error }}</span>
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
    </el-table>

    <!-- 启动采样对话框 -->
    <el-dialog v-model="startVisible" :title="`启动采样 · ${startDeviceId}`" width="420px" destroy-on-close>
      <el-form label-position="top" @submit.prevent="startSampler">
        <el-form-item label="采样周期 (ms)">
          <el-input-number v-model="startInterval" :min="50" :max="60000" :step="100" style="width:100%" />
        </el-form-item>
        <p class="sub">采样器按周期对设备做 snapshot 并写入持久化采样环；重启后不会自动恢复运行。</p>
      </el-form>
      <template #footer>
        <el-button @click="startVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="startSampler">启动</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api/client'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const overview = ref({})
const devices = ref([])
const samples = ref([])
const loading = ref(false)
const acting = ref(false)
const autoRefresh = ref(true)
const sortBy = ref('')
const sampleFilter = ref('')
const startVisible = ref(false)
const startDeviceId = ref('')
const startInterval = ref(1000)
let timer = null

function stateLabel(s) {
  return { idle: '空闲', running: '采样中', stopping: '停止中' }[s] || '空闲'
}

function stateTag(s) {
  return { idle: 'info', running: 'success', stopping: 'warning' }[s] || 'info'
}

function fmtTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  return isNaN(d) ? t : d.toLocaleString()
}

async function loadOverview() {
  const { data } = await api.get('/diagnostics')
  overview.value = data
}

async function loadDevices() {
  const { data } = await api.get('/devices', { params: sortBy.value ? { sort: sortBy.value } : {} })
  devices.value = data.devices || []
}

async function loadSamples() {
  const { data } = await api.get('/diagnostics/samples', {
    params: { limit: 100, ...(sampleFilter.value ? { deviceId: sampleFilter.value } : {}) }
  })
  samples.value = data.samples || []
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([loadOverview(), loadDevices(), loadSamples()])
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '加载诊断信息失败')
  } finally {
    loading.value = false
  }
}

function openStart(deviceId) {
  startDeviceId.value = deviceId
  startInterval.value = 1000
  startVisible.value = true
}

async function startSampler() {
  acting.value = true
  try {
    await api.post(`/diagnostics/${startDeviceId.value}/start`, { intervalMs: startInterval.value })
    ElMessage.success('采样已启动')
    startVisible.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '启动失败')
  } finally {
    acting.value = false
  }
}

async function stopSampler(deviceId) {
  acting.value = true
  try {
    await api.post(`/diagnostics/${deviceId}/stop`)
    ElMessage.success('已请求停止')
    await loadAll()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '停止失败')
  } finally {
    acting.value = false
  }
}

function setupTimer() {
  if (timer) clearInterval(timer)
  timer = null
  if (autoRefresh.value) {
    timer = setInterval(() => { loadAll() }, 2000)
  }
}

watch(autoRefresh, setupTimer)
onMounted(() => {
  loadAll()
  setupTimer()
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>
