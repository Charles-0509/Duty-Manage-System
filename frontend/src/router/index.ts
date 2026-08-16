import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const chunkReloadKey = 'dms_chunk_reload_path'

function recoverFromChunkLoadError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error || '')
  if (!/dynamically imported module|module script failed|unable to preload css/i.test(message)) {
    return
  }

  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`
  try {
    if (sessionStorage.getItem(chunkReloadKey) === currentPath) {
      sessionStorage.removeItem(chunkReloadKey)
      return
    }
    sessionStorage.setItem(chunkReloadKey, currentPath)
  } catch {
    // Reloading still gives the browser a chance to fetch the latest index manifest.
  }
  window.location.reload()
}

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('@/layouts/AppLayout.vue'),
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'availability', name: 'availability', component: () => import('@/views/AvailabilityView.vue') },
        { path: 'finance', name: 'finance', component: () => import('@/views/FinanceView.vue') },
        {
          path: 'labor-convert',
          name: 'labor-convert',
          component: () => import('@/views/LaborConvertView.vue'),
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'schedule',
          name: 'schedule',
          component: () => import('@/views/ScheduleView.vue'),
          meta: { roles: ['ADMIN', 'OWNER', 'HR'] },
        },
        {
          path: 'final-schedule',
          name: 'final-schedule',
          component: () => import('@/views/FinalScheduleView.vue'),
          meta: { roles: ['ADMIN', 'OWNER', 'HR'] },
        },
        {
          path: 'work-orders',
          name: 'work-orders',
          component: () => import('@/views/WorkOrdersView.vue'),
          meta: { roles: ['ADMIN', 'OWNER', 'HR', 'LEADER', 'FINANCE'] },
        },
        {
          path: 'users',
          name: 'users',
          component: () => import('@/views/UsersView.vue'),
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'audit-logs',
          name: 'audit-logs',
          component: () => import('@/views/AuditLogsView.vue'),
          meta: { roles: ['ADMIN'] },
        },
        {
          path: 'system-settings',
          name: 'system-settings',
          component: () => import('@/views/SystemSettingsView.vue'),
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

window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault()
  recoverFromChunkLoadError((event as Event & { payload?: unknown }).payload)
})

router.onError(recoverFromChunkLoadError)

router.afterEach(() => {
  try {
    sessionStorage.removeItem(chunkReloadKey)
  } catch {
    // Session storage may be unavailable in restricted browser contexts.
  }
})

export default router
