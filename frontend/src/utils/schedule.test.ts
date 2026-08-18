import { describe, expect, it } from 'vitest'
import {
  buildAvailabilityCells,
  buildShiftOptionsByCode,
  buildVisibleScheduleByCode,
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
})
