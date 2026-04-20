import { createRouter, createWebHashHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

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
          path: 'schedule',
          name: 'schedule',
          component: () => import('@/views/ScheduleView.vue'),
          meta: { roles: ['ADMIN', 'OWNER'] },
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
          meta: { roles: ['ADMIN', 'OWNER', 'HR', 'LEADER'] },
        },
        {
          path: 'users',
          name: 'users',
          component: () => import('@/views/UsersView.vue'),
          meta: { roles: ['ADMIN'] },
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

export default router
