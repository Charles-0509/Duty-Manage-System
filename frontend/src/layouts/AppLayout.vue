<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Calendar,
  DataAnalysis,
  Document,
  Expand,
  Fold,
  Menu,
  User as UserIcon,
  SwitchButton,
} from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'

const authStore = useAuthStore()
const metaStore = useMetaStore()
const route = useRoute()
const router = useRouter()

const drawerOpen = ref(false)
const sidebarCollapsed = ref(false)
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
    { path: '/finance', label: '财务统计', icon: Document, show: true },
    { path: '/schedule', label: '计划排班', icon: Document, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR']) },
    { path: '/final-schedule', label: '实际值班调整', icon: Document, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR']) },
    { path: '/work-orders', label: '工单管理', icon: Document, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR', 'LEADER']) },
    { path: '/users', label: '用户管理', icon: UserIcon, show: authStore.hasRole(['ADMIN']) },
  ]
  return items.filter((item) => item.show)
})

const forceChangePassword = computed(() => Boolean(authStore.user?.mustChangePassword))
const sidebarToggleIcon = computed(() => (sidebarCollapsed.value ? Expand : Fold))

onMounted(async () => {
  sidebarCollapsed.value = localStorage.getItem('dms_sidebar_collapsed') === 'true'

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

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
  localStorage.setItem('dms_sidebar_collapsed', String(sidebarCollapsed.value))
}
</script>

<template>
  <div class="layout-shell" :class="{ 'layout-shell--collapsed': sidebarCollapsed }">
    <aside class="sidebar glass-card" :class="{ collapsed: sidebarCollapsed }">
      <button class="collapse-toggle" type="button" @click="toggleSidebar">
        <el-icon><component :is="sidebarToggleIcon" /></el-icon>
      </button>

      <div class="brand" :class="{ compact: sidebarCollapsed }">
        <span v-if="!sidebarCollapsed" class="brand-kicker">机房管理系统</span>
        <h1>机房管理系统</h1>
        <p v-if="!sidebarCollapsed">将排班、工单、财务统计和实际值班调整集中在同一个工作台里。</p>
      </div>

      <nav class="nav-list">
        <button
          v-for="item in navItems"
          :key="item.path"
          class="nav-item"
          :class="{ active: route.path === item.path, compact: sidebarCollapsed }"
          @click="navigate(item.path)"
        >
          <el-icon><component :is="item.icon" /></el-icon>
          <span v-if="!sidebarCollapsed">{{ item.label }}</span>
        </button>
      </nav>

      <div class="sidebar-footer panel-card" :class="{ compact: sidebarCollapsed }">
        <p v-if="!sidebarCollapsed" class="section-label">当前登录</p>
        <div class="sidebar-user" :class="{ compact: sidebarCollapsed }">
          <div v-if="!sidebarCollapsed">
            <strong>{{ authStore.user?.realName }}</strong>
            <p class="muted">{{ metaStore.config?.userRoles?.[authStore.user?.role || 'USER'] || authStore.user?.role }}</p>
          </div>
          <el-button type="danger" plain :icon="SwitchButton" :circle="sidebarCollapsed" @click="logout">
            <span v-if="!sidebarCollapsed">退出</span>
          </el-button>
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
  transition: grid-template-columns 0.24s ease;
}

.layout-shell--collapsed {
  grid-template-columns: 104px minmax(0, 1fr);
}

.sidebar {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 22px;
  padding: 28px;
  transition: padding 0.24s ease;
}

.sidebar.collapsed {
  padding: 24px 14px;
}

.collapse-toggle {
  position: absolute;
  top: 18px;
  right: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: 1px solid rgba(24, 48, 66, 0.08);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.72);
  color: var(--text);
  cursor: pointer;
  transition: 0.2s ease;
}

.collapse-toggle:hover {
  transform: translateY(-1px);
  border-color: rgba(15, 118, 110, 0.2);
}

.brand {
  padding-right: 44px;
}

.brand.compact {
  padding-right: 0;
  text-align: center;
}

.brand h1 {
  margin: 10px 0;
  font-size: 1.8rem;
  line-height: 1.2;
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
  letter-spacing: 0.08em;
}

.nav-list,
.drawer-nav {
  display: grid;
  gap: 10px;
}

.sidebar.collapsed .nav-list {
  justify-items: center;
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

.nav-item.compact {
  justify-content: center;
  width: 100%;
  padding: 14px 0;
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

.sidebar-footer.compact {
  padding: 14px 10px;
}

.sidebar-user {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sidebar-user.compact {
  justify-content: center;
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

  .layout-shell--collapsed {
    grid-template-columns: 1fr;
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
