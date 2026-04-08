<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Calendar, DataAnalysis, Document, Menu, User as UserIcon, SwitchButton } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'

const authStore = useAuthStore()
const metaStore = useMetaStore()
const route = useRoute()
const router = useRouter()

const drawerOpen = ref(false)
const passwordForm = reactive({
  currentPassword: '',
  newPassword: '',
  confirmPassword: '',
  loading: false,
})

const navItems = computed(() => {
  const items = [
    { path: '/dashboard', label: '仪表盘', icon: DataAnalysis, show: true },
    { path: '/availability', label: '值班时间登记', icon: Calendar, show: true },
    { path: '/schedule', label: '管理员排班', icon: Document, show: authStore.hasRole(['ADMIN']) },
    { path: '/final-schedule', label: '实际值班调整', icon: Document, show: authStore.hasRole(['ADMIN', 'HR']) },
    { path: '/work-orders', label: '工单管理', icon: Document, show: true },
    { path: '/users', label: '用户管理', icon: UserIcon, show: authStore.hasRole(['ADMIN']) },
  ]
  return items.filter((item) => item.show)
})

const forceChangePassword = computed(() => Boolean(authStore.user?.mustChangePassword))

onMounted(async () => {
  await metaStore.ensureLoaded()
  if (!authStore.user) {
    try {
      await authStore.refreshMe()
    } catch {
      authStore.logout()
      router.push('/login')
    }
  }
})

async function submitPasswordChange() {
  if (!passwordForm.currentPassword || !passwordForm.newPassword) {
    ElMessage.warning('请填写完整密码信息')
    return
  }
  if (passwordForm.newPassword !== passwordForm.confirmPassword) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }

  passwordForm.loading = true
  try {
    await authStore.changeOwnPassword({
      currentPassword: passwordForm.currentPassword,
      newPassword: passwordForm.newPassword,
    })
    ElMessage.success('密码修改成功')
    passwordForm.currentPassword = ''
    passwordForm.newPassword = ''
    passwordForm.confirmPassword = ''
  } finally {
    passwordForm.loading = false
  }
}

function logout() {
  authStore.logout()
  router.push('/login')
}

function navigate(path: string) {
  drawerOpen.value = false
  router.push(path)
}
</script>

<template>
  <div class="layout-shell">
    <aside class="sidebar glass-card">
      <div class="brand">
        <span class="brand-kicker">Personnel Shift OS</span>
        <h1>机房值班管理平台</h1>
        <p>把排班、工时与实际值班调整统一在一个清晰的工作台里。</p>
      </div>

      <nav class="nav-list">
        <button
          v-for="item in navItems"
          :key="item.path"
          class="nav-item"
          :class="{ active: route.path === item.path }"
          @click="navigate(item.path)"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.label }}</span>
        </button>
      </nav>

      <div class="sidebar-footer panel-card">
        <p class="section-label">当前登录</p>
        <div class="sidebar-user">
          <div>
            <strong>{{ authStore.user?.realName }}</strong>
            <p class="muted">{{ metaStore.config?.userRoles?.[authStore.user?.role || 'USER'] || authStore.user?.role }}</p>
          </div>
          <el-button type="danger" plain :icon="SwitchButton" @click="logout">退出</el-button>
        </div>
      </div>
    </aside>

    <section class="main-shell">
      <header class="mobile-header glass-card">
        <div>
          <p class="section-label">导航</p>
          <strong>{{ authStore.user?.realName }}</strong>
        </div>
        <el-button :icon="Menu" circle @click="drawerOpen = true" />
      </header>

      <main class="content-shell">
        <router-view />
      </main>
    </section>

    <el-drawer v-model="drawerOpen" title="功能导航" direction="ltr" size="280px">
      <div class="drawer-nav">
        <button
          v-for="item in navItems"
          :key="item.path"
          class="nav-item"
          :class="{ active: route.path === item.path }"
          @click="navigate(item.path)"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span>{{ item.label }}</span>
        </button>
      </div>
    </el-drawer>

    <el-dialog
      :model-value="forceChangePassword"
      :show-close="false"
      :close-on-click-modal="false"
      width="460px"
      title="首次登录请修改密码"
    >
      <el-form label-position="top">
        <el-form-item label="当前密码">
          <el-input v-model="passwordForm.currentPassword" show-password />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="passwordForm.newPassword" show-password />
        </el-form-item>
        <el-form-item label="确认新密码">
          <el-input v-model="passwordForm.confirmPassword" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button type="primary" :loading="passwordForm.loading" @click="submitPasswordChange">
          完成修改
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.layout-shell {
  display: grid;
  min-height: 100vh;
  gap: 22px;
  grid-template-columns: 320px minmax(0, 1fr);
  padding: 22px;
}

.sidebar {
  display: flex;
  flex-direction: column;
  gap: 22px;
  padding: 28px;
}

.brand h1 {
  margin: 10px 0;
  font-size: 2.1rem;
  line-height: 1.05;
}

.brand p {
  margin: 0;
  color: var(--muted);
  line-height: 1.7;
}

.brand-kicker {
  display: inline-flex;
  padding: 8px 12px;
  border-radius: 999px;
  background: rgba(15, 118, 110, 0.12);
  color: var(--primary);
  font-size: 0.78rem;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}

.nav-list,
.drawer-nav {
  display: grid;
  gap: 10px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border: 1px solid transparent;
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.58);
  color: var(--text);
  cursor: pointer;
  font: inherit;
  transition: 0.2s ease;
}

.nav-item.active,
.nav-item:hover {
  border-color: rgba(15, 118, 110, 0.18);
  background: linear-gradient(135deg, rgba(15, 118, 110, 0.12), rgba(249, 115, 22, 0.08));
  transform: translateY(-1px);
}

.sidebar-footer {
  margin-top: auto;
  padding: 18px;
}

.sidebar-user {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sidebar-user p {
  margin: 6px 0 0;
}

.main-shell {
  min-width: 0;
}

.mobile-header {
  display: none;
  align-items: center;
  justify-content: space-between;
  padding: 18px 20px;
}

.content-shell {
  min-width: 0;
}

@media (max-width: 980px) {
  .layout-shell {
    grid-template-columns: 1fr;
    padding: 14px;
  }

  .sidebar {
    display: none;
  }

  .mobile-header {
    display: flex;
    margin-bottom: 14px;
  }
}
</style>
