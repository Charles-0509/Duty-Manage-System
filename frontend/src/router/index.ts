import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 路由组件懒加载与静态导入映射（支持预加载）
const routeComponents = {
  Login: () => import('@/views/LoginView.vue'),
  Dashboard: () => import('@/views/DashboardView.vue'),
  Availability: () => import('@/views/AvailabilityView.vue'),
  Finance: () => import('@/views/FinanceView.vue'),
  LaborConvert: () => import('@/views/LaborConvertView.vue'),
  Schedule: () => import('@/views/ScheduleView.vue'),
  FinalSchedule: () => import('@/views/FinalScheduleView.vue'),
  WorkOrders: () => import('@/views/WorkOrdersView.vue'),
  Users: () => import('@/views/UsersView.vue'),
  AuditLogs: () => import('@/views/AuditLogsView.vue'),
  SystemSettings: () => import('@/views/SystemSettingsView.vue'),
}

// 预加载所有异步视图组件，消除首次点击菜单不跟手/延迟的问题
export function preloadAllRouteComponents() {
  if (typeof window === 'undefined') return
  const preloader = () => {
    Object.values(routeComponents).forEach((loader) => {
      try {
        loader()
      } catch {}
    })
  }

  if ('requestIdleCallback' in window) {
    ;(window as any).requestIdleCallback(preloader, { timeout: 2000 })
  } else {
    setTimeout(preloader, 300)
  }
}

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: routeComponents.Login,
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('@/layouts/AppLayout.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: routeComponents.Dashboard },
        { path: 'availability', name: 'availability', component: routeComponents.Availability },
        { path: 'finance', name: 'finance', component: routeComponents.Finance },
        {
          path: 'labor-convert',
          name: 'labor-convert',
          component: routeComponents.LaborConvert,
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'schedule',
          name: 'schedule',
          component: routeComponents.Schedule,
          meta: { roles: ['ADMIN', 'OWNER', 'HR'] },
        },
        {
          path: 'final-schedule',
          name: 'final-schedule',
          component: routeComponents.FinalSchedule,
          meta: { roles: ['ADMIN', 'OWNER', 'HR'] },
        },
        {
          path: 'work-orders',
          name: 'work-orders',
          component: routeComponents.WorkOrders,
          meta: { roles: ['ADMIN', 'OWNER', 'HR', 'LEADER', 'FINANCE'] },
        },
        {
          path: 'users',
          name: 'users',
          component: routeComponents.Users,
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'audit-logs',
          name: 'audit-logs',
          component: routeComponents.AuditLogs,
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'system-settings',
          name: 'system-settings',
          component: routeComponents.SystemSettings,
          meta: { roles: ['ADMIN', 'OWNER'] },
        },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const authStore = useAuthStore()
  authStore.hydrate()

  if (to.meta.public) {
    if (authStore.isAuthenticated && to.path === '/login') {
      return '/dashboard'
    }
    return true
  }

  if (!authStore.isAuthenticated) {
    return '/login'
  }

  if (!authStore.user) {
    try {
      await authStore.refreshMe()
    } catch {
      authStore.logout()
      return '/login'
    }
  }

  const roles = to.meta.roles as string[] | undefined
  if (roles?.length && !authStore.hasRole(roles)) {
    return '/dashboard'
  }

  return true
})

// 登录成功后或页面空闲时触发预加载
router.afterEach(() => {
  preloadAllRouteComponents()
})

window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault()
  window.location.reload()
})

export default router
