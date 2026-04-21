<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchSystemSettings, updateSystemSettings } from '@/api/services'
import type { SystemSettings, UpdateSystemSettingsPayload } from '@/types'

const loading = ref(false)
const saving = ref(false)
const currentSettings = ref<SystemSettings | null>(null)

const form = reactive<UpdateSystemSettingsPayload>({
  databasePath: '',
  privateMembersPath: '',
  firstMonday: '',
  syncEnabled: false,
  syncToken: '',
})

const readOnlyItems = computed(() => {
  if (!currentSettings.value) {
    return []
  }

  return [
    {
      label: 'APP_PORT',
      value: currentSettings.value.appPort,
      note: '当前服务监听端口。页面只展示，不直接修改。',
    },
    {
      label: '.env 文件位置',
      value: currentSettings.value.envFilePath,
      note: '当前页面保存时会直接写入这个文件。',
    },
  ]
})

onMounted(async () => {
  await loadSettings()
})

async function loadSettings() {
  loading.value = true
  try {
    const settings = await fetchSystemSettings()
    currentSettings.value = settings
    form.databasePath = settings.databasePath
    form.privateMembersPath = settings.privateMembersPath
    form.firstMonday = settings.firstMonday
    form.syncEnabled = settings.syncEnabled
    form.syncToken = settings.syncToken
  } catch {
    ElMessage.error('加载系统设置失败')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await updateSystemSettings({
      databasePath: form.databasePath.trim(),
      privateMembersPath: form.privateMembersPath.trim(),
      firstMonday: form.firstMonday.trim(),
      syncEnabled: form.syncEnabled,
      syncToken: form.syncToken.trim(),
    })
    await loadSettings()
    ElMessage.success('系统设置已保存，重启 dms.service 后生效')
  } catch {
    ElMessage.error('保存系统设置失败')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">System</p>
        <h2 class="page-title">系统设置</h2>
        <p class="page-subtitle">维护常用运行参数。保存后需要重启 `dms.service` 才会生效。</p>
      </div>
      <div class="toolbar-actions">
        <el-button type="primary" :loading="saving" @click="saveSettings">保存设置</el-button>
      </div>
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
        </el-form>
      </article>

      <article class="glass-card">
        <div class="card-header">
          <div>
            <p class="section-label">Readonly</p>
            <h3>当前环境信息</h3>
          </div>
          <span class="pill">单实例部署</span>
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
          <p class="muted">保存设置只会修改 .env 文件，不会自动重启服务。部署方式已回归单实例 dms.service。</p>
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

.tip-box {
  margin-top: 24px;
  padding: 18px;
  border-radius: 18px;
  background: rgba(15, 118, 110, 0.08);
}
</style>
