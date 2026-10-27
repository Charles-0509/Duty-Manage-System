// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    hydrate: vi.fn(),
    isAuthenticated: true,
    user: { role: 'VIEWER' },
    hasRole: (roles: string[]) => roles.includes('VIEWER'),
  }),
}))
vi.mock('@/views/DashboardView.vue', () => ({ default: {} }))
vi.mock('@/layouts/AppLayout.vue', () => ({ default: {} }))

import router from './index'

describe('dashboard-only viewer navigation', () => {
  it('allows the dashboard and redirects every other page to it', async () => {
    await router.push('/dashboard')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    for (const path of ['/availability', '/finance', '/monthly-reports', '/schedule', '/final-schedule', '/work-orders', '/users', '/audit-logs', '/system-settings', '/login']) {
      await router.push(path)
      expect(router.currentRoute.value.path, path).toBe('/dashboard')
    }
  })
})
