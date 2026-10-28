// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'
import {
  buildAvailabilityCells,
  buildShiftOptionsByCode,
  buildShiftStats,
  buildVisibleScheduleByCode,
  downloadBlob,
  parsePastedSessions,
  validateWorkSessionDates,
} from './schedule'
import type { AvailabilityOverviewItem } from '@/types'

const availability: AvailabilityOverviewItem[] = [
  {
    username: 'member-a',
    realName: '成员甲',
    availability: {
      single: ['Mon-1'],
      double: ['Mon-1', 'Tue-1'],
    },
  },
  {
    username: 'member-b',
    realName: '成员乙',
    availability: {
      single: ['Tue-1'],
      double: [],
    },
  },
]

describe('pasted work-session dates', () => {
  it('rejects a cleared date picker', () => {
    expect(() => validateWorkSessionDates([{ date: null as unknown as string, workerName: '成员甲', duration: 1 }])).toThrow('日期不能为空')
  })
  it.each(['', 'Invalid Date', '2026年2月30日', '2026-13-01', '不是日期'])('rejects %j instead of substituting today or rolling over', (date) => {
    expect(() => parsePastedSessions(`成员甲\t安装软件\t${date}\t1`)).toThrow(/日期/)
  })
  it('preserves valid imported dates and hours', () => {
    expect(parsePastedSessions('成员甲\t安装软件\t2026/10/12\t1')).toEqual([{ workerName: '成员甲', date: '2026-10-12', duration: 1 }])
  })
})

it('keeps the three download URLs alive until queued downloads can read them', () => {
  vi.useFakeTimers()
  const create = vi.fn().mockReturnValueOnce('blob:one').mockReturnValueOnce('blob:two').mockReturnValueOnce('blob:three')
  const revoke = vi.fn()
  vi.stubGlobal('URL', { createObjectURL: create, revokeObjectURL: revoke })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  try {
    for (const filename of ['劳务计算.xlsx', '劳务勤助转换.xlsx', '记录表.zip']) downloadBlob(new Blob(['data']), filename)
    expect(click).toHaveBeenCalledTimes(3)
    expect(revoke).not.toHaveBeenCalled()
    expect(document.querySelectorAll('a[download]')).toHaveLength(0)
    vi.advanceTimersByTime(60_000)
    expect(revoke).toHaveBeenCalledTimes(3)
  } finally {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.useRealTimers()
  }
})

describe('schedule matrix indexes', () => {
  it('builds availability cells once for each visible week mode', () => {
    expect(buildAvailabilityCells(availability, 'all')).toEqual({
      'Mon-1': [{ name: '成员甲', tone: 'both' }],
      'Tue-1': [
        { name: '成员甲', tone: 'double' },
        { name: '成员乙', tone: 'single' },
      ],
    })
    expect(buildAvailabilityCells(availability, 'single')).toEqual({
      'Mon-1': [{ name: '成员甲', tone: 'both' }],
      'Tue-1': [{ name: '成员乙', tone: 'single' }],
    })
    expect(buildAvailabilityCells(availability, 'double')).toEqual({
      'Mon-1': [{ name: '成员甲', tone: 'both' }],
      'Tue-1': [{ name: '成员甲', tone: 'double' }],
    })
  })

  it('builds stable editor options without rescanning members per cell', () => {
    expect(buildShiftOptionsByCode(availability)).toEqual({
      'Mon-1': [
        { label: '成员甲(单)', value: '成员甲(单)' },
        { label: '成员甲(双)', value: '成员甲(双)' },
      ],
      'Tue-1': [
        { label: '成员甲(双)', value: '成员甲(双)' },
        { label: '成员乙(单)', value: '成员乙(单)' },
      ],
    })
  })

  it('normalizes visible schedule labels once per shift', () => {
    const schedule = {
      'Mon-1': ['成员甲(单)', '成员甲(双)', '成员乙(单)'],
      'Tue-1': ['成员乙(双)'],
    }

    expect(buildVisibleScheduleByCode(schedule, 'all')).toEqual({
      'Mon-1': ['成员甲(单双)', '成员乙(单)'],
      'Tue-1': ['成员乙(双)'],
    })
    expect(buildVisibleScheduleByCode(schedule, 'single')).toEqual({
      'Mon-1': ['成员甲(单)', '成员乙(单)'],
      'Tue-1': [],
    })
  })

  it('calculates shift statistics from the current editor draft', () => {
    expect(buildShiftStats({
      'Mon-1': ['成员甲(单)', '成员甲(双)', '成员乙(单双)'],
      'Tue-1': ['成员甲(单)'],
    })).toEqual([
      { name: '成员甲', value: 1.5 },
      { name: '成员乙', value: 1 },
    ])
  })
})
