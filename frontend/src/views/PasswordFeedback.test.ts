// @vitest-environment jsdom
import { createApp, nextTick, type App, type Component } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  changeOwnPassword: vi.fn(),
  resetUserPassword: vi.fn(),
  error: vi.fn(),
  success: vi.fn(),
}))
vi.mock('element-plus', async importOriginal => ({
  ...await importOriginal<typeof import('element-plus')>(),
  ElMessage: { error: mocks.error, success: mocks.success, warning: vi.fn() },
}))
vi.mock('vue-router', () => ({ useRoute: () => ({ path: '/dashboard' }), useRouter: () => ({ push: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { username: 'member', realName: '成员甲', role: 'USER', mustChangePassword: true },
    hasRole: (roles: string[]) => roles.includes('USER'),
    changeOwnPassword: mocks.changeOwnPassword,
  }),
}))
vi.mock('@/stores/meta', () => ({
  useMetaStore: () => ({ ensureLoaded: async () => {}, config: { semester: { name: '测试学期' }, userRoles: {} } }),
}))
vi.mock('@/api/services', () => ({
  fetchUsers: vi.fn(async () => [{ id: 2, realName: '成员甲', role: 'USER', isActive: true, semesterMember: true }]),
  resetUserPassword: mocks.resetUserPassword,
  createUser: vi.fn(), removeUserMembership: vi.fn(), restoreUserMembership: vi.fn(), updateUserProfile: vi.fn(), updateUserStatus: vi.fn(),
}))
import AppLayout from '@/layouts/AppLayout.vue'
import UsersView from './UsersView.vue'

vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
let app: App | undefined
let root: HTMLDivElement | undefined
async function mount(component: Component) {
  root = document.createElement('div')
  document.body.appendChild(root)
  app = createApp(component)
  app.directive('loading', {})
  app.component('router-view', { template: '<div />' })
  app.config.errorHandler = () => {}
  app.mount(root)
  await nextTick()
}
function click(text: string) {
  const button = [...document.querySelectorAll('button')].find(node => node.textContent?.trim() === text)
  expect(button).toBeDefined()
  button!.click()
}
async function fill(input: HTMLInputElement, value: string) {
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await nextTick()
}
afterEach(async () => {
  app?.unmount()
  await nextTick()
  root?.remove()
  vi.resetAllMocks()
})

describe('password validation feedback', () => {
  it.each(['密码不符合安全要求：至少需要 6 个字符', '密码不符合安全要求：需包含大写字母、小写字母、数字、特殊字符中的至少两种'])('shows own-password API rejection: %s', async message => {
    mocks.changeOwnPassword.mockRejectedValueOnce({ response: { data: { message } } })
    await mount(AppLayout)
    const inputs = document.querySelectorAll<HTMLInputElement>('.el-dialog input')
    await fill(inputs[0]!, 'old-password')
    await fill(inputs[1]!, 'abcdef')
    await fill(inputs[2]!, 'abcdef')
    click('完成修改')
    await vi.waitFor(() => expect(mocks.error).toHaveBeenCalledWith(message))
    expect(inputs[1]!.value).toBe('abcdef')
    expect(mocks.success).not.toHaveBeenCalled()
  })

  it.each(['密码不符合安全要求：至少需要 6 个字符', undefined])('shows admin-reset rejection and keeps input (%s)', async message => {
    mocks.resetUserPassword.mockRejectedValueOnce(message ? { response: { data: { message } } } : new Error('Network Error'))
    await mount(UsersView)
    await vi.waitFor(() => expect(document.querySelector('.el-table tbody')?.textContent).toContain('成员甲'))
    click('管理')
    await nextTick()
    const input = document.querySelector<HTMLInputElement>('.el-drawer input[type="password"]')!
    await fill(input, 'abc')
    click('重置密码')
    await vi.waitFor(() => expect(mocks.error).toHaveBeenCalledWith(message || '重置密码失败，请稍后重试'))
    expect(input.value).toBe('abc')
    expect(mocks.success).not.toHaveBeenCalled()
  })
})
