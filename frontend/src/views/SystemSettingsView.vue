<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchSystemSettings, triggerHotUpdate, updateSystemSettings } from '@/api/services'
import type { HotUpdateStartResponse, SystemSettings, UpdateSystemSettingsPayload } from '@/types'

const loading = ref(false)
const saving = ref(false)
const deploying = ref(false)
const autoRefreshPending = ref(false)
const refreshCountdown = ref(0)
const currentSettings = ref<SystemSettings | null>(null)

const form = reactive<UpdateSystemSettingsPayload>({
  databasePath: '',
  privateMembersPath: '',
  firstMonday: '',
  syncEnabled: false,
  syncToken: '',
  hotSwitchDrainSeconds: '',
})

let countdownTimer: number | null = null
let pollTimer: number | null = null

const readOnlyItems = computed(() => {
  if (!currentSettings.value) {
    return []
  }

  return [
    {
      label: '对外端口 APP_PORT',
      value: currentSettings.value.appPort,
      note: '当前热更新代理监听的端口。这个值通常通过 systemd 和热更新栈统一管理，不在页面内修改。',
    },
    {
      label: 'Blue 槽位端口',
      value: currentSettings.value.hotSlotBluePort,
      note: '蓝绿发布内部端口，仅供查看。',
    },
    {
      label: 'Green 槽位端口',
      value: currentSettings.value.hotSlotGreenPort,
      note: '蓝绿发布内部端口，仅供查看。',
    },
    {
      label: '.env 文件位置',
      value: currentSettings.value.envFilePath,
      note: '页面保存时会直接写入这个文件。',
    },
  ]
})

const hotUpdateSupported = computed(() => currentSettings.value?.hotUpdateSupported ?? false)

onMounted(async () => {
  await loadSettings()
})

onBeforeUnmount(() => {
  clearTimers()
})

async function loadSettings() {
  loading.value = true
  try {
    const settings = await fetchSystemSettings()
    currentSettings.value = settings
    syncForm(settings)
  } catch {
    ElMessage.error('加载系统设置失败')
  } finally {
    loading.value = false
  }
}

function syncForm(settings: SystemSettings) {
  form.databasePath = settings.databasePath
  form.privateMembersPath = settings.privateMembersPath
  form.firstMonday = settings.firstMonday
  form.syncEnabled = settings.syncEnabled
  form.syncToken = settings.syncToken
  form.hotSwitchDrainSeconds = settings.hotSwitchDrainSeconds
}

async function saveSettings(silent = false) {
  saving.value = true
  try {
    await updateSystemSettings({
      databasePath: form.databasePath.trim(),
      privateMembersPath: form.privateMembersPath.trim(),
      firstMonday: form.firstMonday.trim(),
      syncEnabled: form.syncEnabled,
      syncToken: form.syncToken.trim(),
      hotSwitchDrainSeconds: form.hotSwitchDrainSeconds.trim(),
    })
    await loadSettings()
    if (!silent) {
      ElMessage.success('系统设置已保存，需更新服务后才会生效')
    }
  } finally {
    saving.value = false
  }
}

async function saveAndDeploy() {
  if (!hotUpdateSupported.value) {
    ElMessage.error('当前环境不支持网页触发热更新')
    return
  }

  deploying.value = true
  try {
    await saveSettings(true)
    const result = await triggerHotUpdate()
    ElMessage.success('热更新已启动，页面会在服务恢复后自动刷新')
    beginAutoRefresh(result)
  } catch {
    deploying.value = false
    ElMessage.error('触发热更新失败')
  }
}

function beginAutoRefresh(result: HotUpdateStartResponse) {
  clearTimers()
  autoRefreshPending.value = true
  refreshCountdown.value = result.refreshDelay

  countdownTimer = window.setInterval(() => {
    if (refreshCountdown.value > 0) {
      refreshCountdown.value -= 1
      return
    }

    if (countdownTimer !== null) {
      window.clearInterval(countdownTimer)
      countdownTimer = null
    }

    startHealthPolling(result)
  }, 1000)
}

function startHealthPolling(result: HotUpdateStartResponse) {
  const runCheck = async () => {
    try {
      const response = await fetch(`${result.healthPath}?ts=${Date.now()}`, {
        cache: 'no-store',
        credentials: 'same-origin',
      })
      if (response.ok) {
        window.location.reload()
      }
    } catch {
      // wait for the next poll
    }
  }

  void runCheck()
  pollTimer = window.setInterval(() => {
    void runCheck()
  }, Math.max(result.pollInterval, 1) * 1000)
}

function clearTimers() {
  if (countdownTimer !== null) {
    window.clearInterval(countdownTimer)
    countdownTimer = null
  }
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">System</p>
        <h2 class="page-title">系统设置</h2>
        <p class="page-subtitle">维护常用运行参数，并可直接从页面触发 Linux 热更新。</p>
      </div>
      <div class="toolbar-actions">
        <el-button :loading="saving" @click="saveSettings()">保存设置</el-button>
        <el-button type="primary" :loading="deploying" :disabled="!hotUpdateSupported" @click="saveAndDeploy">
          更新服务
        </el-button>
      </div>
    </section>

    <section v-if="autoRefreshPending" class="glass-card status-banner">
      <strong>热更新进行中</strong>
      <p class="muted">
        {{ refreshCountdown > 0 ? `预计 ${refreshCountdown} 秒后开始探测新服务。` : '正在等待新服务恢复，恢复后会自动刷新页面。' }}
      </p>
    </section>

    <section class="data-grid settings-grid">
      <article class="glass-card">
        <div class="card-header">
          <div>
            <p class="section-label">Editable</p>
            <h3>可修改项</h3>
          </div>
          <span class="pill">保存到 backend/.env</span>
        </div>

        <el-form label-position="top" class="settings-form">
          <el-form-item label="数据库路径 DATABASE_PATH">
            <el-input v-model="form.databasePath" placeholder="../data/personnel.db" />
          </el-form-item>
          <el-form-item label="成员文件路径 PRIVATE_MEMBERS_PATH">
            <el-input v-model="form.privateMembersPath" placeholder="../data/member.json" />
          </el-form-item>
          <el-form-item label="单双周起始 FIRST_MONDAY">
            <el-input v-model="form.firstMonday" placeholder="20260302" maxlength="8" />
          </el-form-item>
          <el-form-item label="同步功能 SYNC_ENABLED">
            <el-switch v-model="form.syncEnabled" />
          </el-form-item>
          <el-form-item label="同步口令 SYNC_TOKEN">
            <el-input v-model="form.syncToken" show-password placeholder="同步开启时必填" />
          </el-form-item>
          <el-form-item label="切换排空秒数 HOT_SWITCH_DRAIN_SECONDS">
            <el-input v-model="form.hotSwitchDrainSeconds" placeholder="5" />
          </el-form-item>
        </el-form>
      </article>

      <article class="glass-card">
        <div class="card-header">
          <div>
            <p class="section-label">Readonly</p>
            <h3>当前环境信息</h3>
          </div>
          <span class="pill" :class="{ 'pill--warn': !hotUpdateSupported }">
            {{ hotUpdateSupported ? '支持网页热更新' : '当前环境不支持网页热更新' }}
          </span>
        </div>

        <div class="readonly-list">
          <div v-for="item in readOnlyItems" :key="item.label" class="readonly-item">
            <p class="section-label">{{ item.label }}</p>
            <strong>{{ item.value }}</strong>
            <p class="muted">{{ item.note }}</p>
          </div>
        </div>

        <div class="tip-box">
          <p class="section-label">说明</p>
          <p class="muted">页面不会暴露 JWT_SECRET 和默认管理员密码。这两项属于高风险配置，仍建议只在服务器文件中手动维护。</p>
          <p class="muted">保存设置只会修改 .env 文件；真正生效需要点击“更新服务”或由服务器重启后重新加载。</p>
        </div>
      </article>
    </section>
  </div>
</template>

<style scoped>
.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.settings-grid {
  align-items: start;
}

.glass-card {
  padding: 24px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
  margin-bottom: 18px;
}

.settings-form {
  display: grid;
  gap: 4px;
}

.readonly-list {
  display: grid;
  gap: 16px;
}

.readonly-item strong {
  display: block;
  margin-bottom: 6px;
  word-break: break-all;
}

.status-banner {
  display: grid;
  gap: 6px;
}

.tip-box {
  margin-top: 24px;
  padding: 18px;
  border-radius: 18px;
  background: rgba(15, 118, 110, 0.08);
}

.pill--warn {
  background: rgba(249, 115, 22, 0.14);
  color: #c2410c;
}
</style>
