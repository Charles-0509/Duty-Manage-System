<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Calendar,
  DataAnalysis,
  DocumentCopy,
  Expand,
  Fold,
  Management,
  Money,
  Operation,
  Setting,
  Tickets,
  User as UserIcon,
  SwitchButton,
} from '@element-plus/icons-vue'
import AppLogo from '@/components/AppLogo.vue'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import type { Role } from '@/types'

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

interface NavGroup {
  name?: string
  items: {
    path: string
    label: string
    icon: any
    show: boolean
  }[]
}

const navGroups = computed<NavGroup[]>(() => {
  const groups: NavGroup[] = [
    {
      name: '总览与统计',
      items: [
        { path: '/dashboard', label: '仪表盘', icon: DataAnalysis, show: true },
        { path: '/finance', label: '财务统计', icon: Money, show: true },
        { path: '/work-orders', label: '工单管理', icon: Tickets, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR', 'LEADER', 'FINANCE']) },
      ],
    },
    {
      name: '排班调度',
      items: [
        { path: '/availability', label: '值班时间登记', icon: Calendar, show: true },
        { path: '/schedule', label: '计划排班', icon: Operation, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR']) },
        { path: '/final-schedule', label: '实际值班调整', icon: Management, show: authStore.hasRole(['ADMIN', 'OWNER', 'HR']) },
      ],
    },
    {
      name: '系统与运维',
      items: [
        { path: '/labor-convert', label: '劳务转换', icon: DocumentCopy, show: authStore.hasRole(['ADMIN']) },
        { path: '/users', label: '用户管理', icon: UserIcon, show: authStore.hasRole(['ADMIN']) },
        { path: '/audit-logs', label: '审计日志', icon: DocumentCopy, show: authStore.hasRole(['ADMIN']) },
        { path: '/system-settings', label: '系统设置', icon: Setting, show: authStore.hasRole(['ADMIN', 'OWNER']) },
      ],
    },
  ]

  return groups
    .map((g) => ({
      ...g,
      items: g.items.filter((item) => item.show),
    }))
    .filter((g) => g.items.length > 0)
})

const userRoleName = computed(() => {
  const role = authStore.user?.role as Role | undefined
  if (!role || !metaStore.config?.userRoles) return role || ''
  return metaStore.config.userRoles[role] || role
})

const forceChangePassword = computed(() => Boolean(authStore.user?.mustChangePassword))
const sidebarToggleIcon = computed(() => (sidebarCollapsed.value ? Expand : Fold))
const activeSemester = computed(() => metaStore.config?.semester)

onMounted(async () => {
  sidebarCollapsed.value = localStorage.getItem('dms_sidebar_collapsed') === 'true'
  await metaStore.ensureLoaded()
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
    <!-- Liquid Glass 悬浮侧边导航 -->
    <aside class="sidebar glass-card" :class="{ collapsed: sidebarCollapsed }">
      <button class="collapse-toggle" type="button" :title="sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'" @click="toggleSidebar">
        <el-icon><component :is="sidebarToggleIcon" /></el-icon>
      </button>

      <div class="brand" :class="{ compact: sidebarCollapsed }">
        <div class="brand-header">
          <AppLogo :size="sidebarCollapsed ? 30 : 34" />
          <div v-if="!sidebarCollapsed" class="brand-info">
            <h1 class="brand-title">机房管理系统</h1>
            <span class="brand-tag">DMS</span>
          </div>
        </div>
        <div v-if="!sidebarCollapsed && activeSemester" class="semester-box">
          <span class="semester-badge">
            <span class="status-dot" :class="{ archived: activeSemester.archived }" />
            {{ activeSemester.name }}<template v-if="activeSemester.archived">（已归档）</template>
          </span>
        </div>
      </div>

      <nav class="nav-container">
        <div v-for="(group, gIdx) in navGroups" :key="gIdx" class="nav-group">
          <p v-if="!sidebarCollapsed && group.name" class="nav-group-title">{{ group.name }}</p>
          <div class="nav-group-list">
            <button
              v-for="item in group.items"
              :key="item.path"
              class="nav-item"
              :class="{ active: route.path === item.path, compact: sidebarCollapsed }"
              :title="sidebarCollapsed ? item.label : undefined"
              @click="navigate(item.path)"
            >
              <el-icon class="nav-icon"><component :is="item.icon" /></el-icon>
              <span v-if="!sidebarCollapsed" class="nav-label">{{ item.label }}</span>
            </button>
          </div>
        </div>
      </nav>

      <div class="sidebar-footer panel-card" :class="{ compact: sidebarCollapsed }">
        <div class="sidebar-user" :class="{ compact: sidebarCollapsed }">
          <div v-if="!sidebarCollapsed" class="user-meta">
            <span class="user-name">{{ authStore.user?.realName || authStore.user?.username }}</span>
            <span class="user-role">{{ userRoleName }}</span>
          </div>
          <el-button
            type="danger"
            link
            class="logout-btn"
            :title="sidebarCollapsed ? '退出登录' : undefined"
            @click="logout"
          >
            <el-icon><SwitchButton /></el-icon>
            <span v-if="!sidebarCollapsed">退出</span>
          </el-button>
        </div>
      </div>
    </aside>

    <section class="main-shell">
      <header class="mobile-header glass-card">
        <div class="mobile-brand-wrap">
          <AppLogo :size="30" />
          <div class="mobile-header-info">
            <strong>机房管理系统</strong>
            <span v-if="activeSemester" class="semester-badge mobile-semester-badge">
              <span class="status-dot" :class="{ archived: activeSemester.archived }" />
              {{ activeSemester.name }}<template v-if="activeSemester.archived">（已归档）</template>
            </span>
          </div>
        </div>
        <div class="mobile-header-actions">
          <el-button text @click="drawerOpen = true">
            <el-icon :size="20"><Fold /></el-icon>
          </el-button>
          <el-button text type="danger" @click="logout">
            <el-icon :size="18"><SwitchButton /></el-icon>
          </el-button>
        </div>
      </header>

      <main class="content-shell">
        <div v-if="activeSemester?.archived" class="archived-banner panel-card">
          当前正在查看归档学期：{{ activeSemester.name }}，业务数据为只读状态。
        </div>
        <router-view />
      </main>
    </section>

    <el-drawer v-model="drawerOpen" title="功能导航" direction="ltr" size="280px">
      <div class="drawer-nav">
        <div v-for="(group, gIdx) in navGroups" :key="gIdx" class="nav-group">
          <p v-if="group.name" class="nav-group-title">{{ group.name }}</p>
          <div class="nav-group-list">
            <button
              v-for="item in group.items"
              :key="item.path"
              class="nav-item"
              :class="{ active: route.path === item.path }"
              @click="navigate(item.path)"
            >
              <el-icon class="nav-icon"><component :is="item.icon" /></el-icon>
              <span>{{ item.label }}</span>
            </button>
          </div>
        </div>
      </div>
    </el-drawer>

    <el-dialog
      :model-value="forceChangePassword"
      :show-close="false"
      :close-on-click-modal="false"
      width="440px"
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
  min-width: 0;
  gap: 20px;
  grid-template-columns: 268px minmax(0, 1fr);
  padding: 20px;
  transition: grid-template-columns 0.24s cubic-bezier(0.4, 0, 0.2, 1);
}

.layout-shell--collapsed {
  grid-template-columns: 84px minmax(0, 1fr);
}

.sidebar {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 24px 18px;
  border-radius: var(--radius-xl);
  transition: padding 0.24s ease;
  height: calc(100vh - 40px);
  position: sticky;
  top: 20px;
  overflow-y: auto;
}

.sidebar.collapsed {
  padding: 24px 12px;
  align-items: center;
}

.collapse-toggle {
  position: absolute;
  top: 20px;
  right: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.9);
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.15s ease;
}

.sidebar.collapsed .collapse-toggle {
  position: static;
  margin-bottom: 12px;
}

.collapse-toggle:hover {
  color: var(--primary);
  border-color: var(--primary);
  background: #ffffff;
}

.brand-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.brand-info {
  display: flex;
  flex-direction: column;
}

.brand-title {
  margin: 0;
  font-size: 1.15rem;
  font-weight: 700;
  color: var(--text);
  line-height: 1.2;
}

.brand-tag {
  font-size: 0.72rem;
  color: #2563eb;
  font-weight: 700;
  letter-spacing: 0.06em;
}

.semester-box {
  margin-top: 12px;
}

.semester-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid rgba(15, 118, 110, 0.16);
  border-radius: 999px;
  background: rgba(15, 118, 110, 0.08);
  color: var(--primary);
  font-size: 0.78rem;
  font-weight: 600;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #16a34a;
}

.status-dot.archived {
  background: #d97706;
}

.archived-banner {
  margin-bottom: 18px;
  padding: 12px 18px;
  border-left: 4px solid #d97706;
  background: #fffbeb;
  color: #92400e;
  font-size: 0.9rem;
  font-weight: 600;
}

.nav-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
  flex: 1;
}

.nav-group-title {
  margin: 0 0 6px 8px;
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--muted);
}

.nav-group-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 10px 14px;
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: 0.92rem;
  font-weight: 500;
  transition: all 0.15s cubic-bezier(0.4, 0, 0.2, 1);
  text-align: left;
}

.nav-item.compact {
  justify-content: center;
  padding: 12px 0;
  border-radius: var(--radius-md);
}

.nav-icon {
  font-size: 1.15rem;
  flex-shrink: 0;
  color: var(--muted);
  transition: color 0.15s ease;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.85);
  color: var(--text);
}

.nav-item:hover .nav-icon {
  color: var(--primary);
}

.nav-item.active {
  background: rgba(255, 255, 255, 0.95);
  color: var(--primary);
  font-weight: 600;
  border-color: rgba(15, 118, 110, 0.16);
  box-shadow: 0 2px 8px rgba(45, 35, 20, 0.04);
}

.nav-item.active .nav-icon {
  color: var(--primary);
}

.sidebar-footer {
  margin-top: auto;
  padding: 12px 14px;
  background: rgba(255, 255, 255, 0.88);
}

.sidebar-footer.compact {
  padding: 10px 6px;
}

.sidebar-user {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.sidebar-user.compact {
  justify-content: center;
}

.user-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.user-name {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.user-role {
  font-size: 0.76rem;
  color: var(--muted);
}

.logout-btn {
  padding: 6px 8px;
  font-size: 0.86rem;
}

.main-shell {
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

.mobile-header {
  display: none;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  margin-bottom: 16px;
  border-radius: var(--radius-lg);
}

.mobile-brand-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.mobile-header-info {
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.mobile-header-info strong {
  display: block;
  font-size: 0.98rem;
  line-height: 1.2;
}

.mobile-semester-badge {
  margin-top: 4px;
  font-size: 0.74rem;
  padding: 2px 8px;
}

.mobile-header-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.content-shell {
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

.drawer-nav {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

@media (max-width: 1024px) {
  .layout-shell {
    grid-template-columns: 1fr;
    padding: 12px;
    gap: 12px;
  }

  .layout-shell--collapsed {
    grid-template-columns: 1fr;
  }

  .sidebar {
    display: none;
  }

  .mobile-header {
    display: flex;
  }
}
</style>
