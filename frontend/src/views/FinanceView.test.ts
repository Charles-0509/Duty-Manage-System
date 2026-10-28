// @vitest-environment jsdom
import { createApp, nextTick, type App } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({ role: 'ADMIN' }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { realName: '值班人员' },
    hasRole: (roles: string[]) => roles.includes(state.role),
  }),
}))
vi.mock('@/stores/meta', () => ({
  useMetaStore: () => ({ ensureLoaded: async () => {}, config: { userNames: ['负责人', '值班人员'] } }),
}))
vi.mock('@/api/services', () => ({
  fetchWorkOrders: vi.fn(async () => []),
  fetchFinanceSummary: vi.fn(async () => ({
    month: '2026-10', dutyHours: 2, dutyAmount: 50, workOrderHours: 1, workOrderAmount: 50,
    managementAmount: 1200, managementPending: false, totalAmount: 1300, workOrderDetails: [],
    memberSummaries: [
      { realName: '负责人', dutyAmount: 0, workOrderAmount: 0, managementAmount: 1200, totalAmount: 1200 },
      { realName: '值班人员', dutyAmount: 50, workOrderAmount: 50, managementAmount: 0, totalAmount: 100 },
    ],
  })),
}))

import { fetchFinanceSummary } from '@/api/services'
import FinanceView from './FinanceView.vue'

vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
let app: App | undefined
let root: HTMLDivElement | undefined

async function mountFinance(role: string) {
  state.role = role
  root = document.createElement('div')
  document.body.appendChild(root)
  app = createApp(FinanceView)
  app.directive('loading', {})
  app.mount(root)
  await vi.waitFor(() => expect(fetchFinanceSummary).toHaveBeenCalled())
  await nextTick()
  return root
}

afterEach(async () => {
  app?.unmount()
  await nextTick()
  root?.remove()
  vi.clearAllMocks()
})

describe('finance member compensation table', () => {
  it.each(['ADMIN', 'OWNER', 'FINANCE'])('defaults to one month of management pay and shows the table for %s', async role => {
    const view = await mountFinance(role)
    expect(fetchFinanceSummary).toHaveBeenCalledWith(expect.objectContaining({ realName: '', includeManagement: true, managementMonths: 1 }))
    const headings = [...view.querySelectorAll('h3')].map(node => node.textContent)
    expect(headings.indexOf('成员酬劳汇总')).toBeGreaterThanOrEqual(0)
    expect(headings.indexOf('成员酬劳汇总')).toBeLessThan(headings.indexOf('工单明细'))
    const columns = [...view.querySelector('.el-table')!.querySelectorAll('thead th')].map(node => node.textContent?.trim())
    expect(columns).toEqual(['姓名', '值班酬劳', '工单酬劳', '项目管理薪酬', '总酬劳'])
    const worker = [...view.querySelector('.el-table')!.querySelectorAll('tbody tr')].find(row => row.textContent?.includes('值班人员'))!
    expect(worker.querySelectorAll('td')[3].textContent?.trim()).toBe('0')
  })

  it.each(['USER', 'LEADER', 'HR'])('never renders other members pay for %s, even if a response contains it', async role => {
    const view = await mountFinance(role)
    expect(view.textContent).not.toContain('成员酬劳汇总')
    expect([...view.querySelectorAll('h3')].map(node => node.textContent)).toContain('工单明细')
  })

  it('hides the aggregate table after selecting a member', async () => {
    const view = await mountFinance('ADMIN')
    const memberSelect = view.querySelectorAll('.el-select')[1]
    memberSelect.querySelector('.el-select__wrapper')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await vi.waitFor(() => expect([...document.querySelectorAll('[role="option"]')].some(node => node.textContent?.trim() === '负责人')).toBe(true))
    const owner = [...document.querySelectorAll('[role="option"]')].find(node => node.textContent?.trim() === '负责人')!
    owner.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await vi.waitFor(() => expect(fetchFinanceSummary).toHaveBeenLastCalledWith(expect.objectContaining({ realName: '负责人' })))
    expect(view.textContent).not.toContain('成员酬劳汇总')
  })

  it('opens the range dialog with management pay checked and one month', async () => {
    const view = await mountFinance('ADMIN')
    const button = [...view.querySelectorAll('button')].find(node => node.textContent?.trim() === '选择日期段')!
    button.click()
    await nextTick()
    expect(document.querySelector<HTMLInputElement>('.el-dialog input[type="checkbox"]')?.checked).toBe(true)
    expect(document.querySelector<HTMLInputElement>('.el-dialog input[role="spinbutton"]')?.value).toBe('1')
  })
})
