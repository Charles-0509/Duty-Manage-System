import dayjs from 'dayjs'
import type { AvailabilityOverviewItem, AvailabilityPayload, WorkSession } from '@/types'

export function buildShiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}

export function baseName(label: string) {
  return label.replace(/\((单|双|单双)\)$/, '')
}

export function normalizeScheduleLabels(labels: string[]) {
  const normalized = labels.flatMap((label) => {
    if (label.endsWith('(单双)')) {
      const name = baseName(label)
      return [`${name}(单)`, `${name}(双)`]
    }

    return [label]
  })

  return Array.from(new Set(normalized))
}

export function tagType(label: string) {
  if (label.endsWith('(单双)')) return 'both'
  if (label.endsWith('(单)')) return 'single'
  if (label.endsWith('(双)')) return 'double'
  return 'plain'
}

export function visibleScheduleNames(labels: string[], mode: 'all' | 'single' | 'double', onlyUser = '') {
  const filtered = labels
    .filter((label) => (onlyUser ? baseName(label) === onlyUser : true))
    .filter((label) => {
      if (mode === 'all') return true
      if (mode === 'single') return label.endsWith('(单)') || label.endsWith('(单双)')
      if (mode === 'double') return label.endsWith('(双)') || label.endsWith('(单双)')
      return true
    })

  if (mode !== 'all') {
    return filtered
  }

  const order: string[] = []
  const states = new Map<string, { single: boolean; double: boolean }>()

  for (const label of filtered) {
    const name = baseName(label)
    if (!states.has(name)) {
      states.set(name, { single: false, double: false })
      order.push(name)
    }

    const state = states.get(name)!
    if (label.endsWith('(单双)')) {
      state.single = true
      state.double = true
    } else if (label.endsWith('(单)')) {
      state.single = true
    } else if (label.endsWith('(双)')) {
      state.double = true
    } else {
      state.single = true
      state.double = true
    }
  }

  return order.map((name) => {
    const state = states.get(name)!
    if (state.single && state.double) {
      return `${name}(单双)`
    }
    if (state.single) {
      return `${name}(单)`
    }
    return `${name}(双)`
  })
}

export function hasAvailability(payload: AvailabilityPayload, shiftCode: string, mode: 'single' | 'double') {
  return mode === 'single' ? payload.single.includes(shiftCode) : payload.double.includes(shiftCode)
}

export function availabilityCellUsers(
  items: AvailabilityOverviewItem[],
  shiftCode: string,
  mode: 'all' | 'single' | 'double',
) {
  return items
    .filter((item) => {
      const single = item.availability.single.includes(shiftCode)
      const double = item.availability.double.includes(shiftCode)
      if (mode === 'all') return single || double
      if (mode === 'single') return single || double
      if (mode === 'double') return double || single
      return false
    })
    .map((item) => ({
      name: item.realName,
      tone: item.availability.single.includes(shiftCode) && item.availability.double.includes(shiftCode)
        ? 'both'
        : item.availability.single.includes(shiftCode)
          ? 'single'
          : 'double',
    }))
}

export function calculateWeekNumber(selectedDate: string, firstMonday: string) {
  const first = dayjs(firstMonday, 'YYYYMMDD')
  const current = dayjs(selectedDate)
  const delta = current.startOf('day').diff(first.startOf('day'), 'day')
  if (delta < 0) return 1
  return Math.floor(delta / 7) + 1
}

const MONTH_RANGE_START = '2026-04'
const MONTH_RANGE_END = '2050-12'

export function monthOptions() {
  const start = dayjs(`${MONTH_RANGE_START}-01`)
  const maxAllowed = dayjs(`${MONTH_RANGE_END}-01`)
  const visibleEnd = dayjs().startOf('month').add(1, 'month')
  const end = visibleEnd.isBefore(maxAllowed) ? visibleEnd : maxAllowed
  const months: string[] = []

  if (end.isBefore(start)) {
    return [MONTH_RANGE_START]
  }

  for (let current = start; current.isBefore(end) || current.isSame(end, 'month'); current = current.add(1, 'month')) {
    months.push(current.format('YYYY-MM'))
  }

  return months
}

export function defaultMonthOption() {
  const current = dayjs().format('YYYY-MM')
  const visibleMonths = monthOptions()

  if (visibleMonths.includes(current)) {
    return current
  }

  if (current < MONTH_RANGE_START) {
    return MONTH_RANGE_START
  }

  return visibleMonths[visibleMonths.length - 1] || MONTH_RANGE_START
}

export function parsePastedSessions(raw: string) {
  return parseTabSeparatedRows(raw)
    .map((parts) => parts.map((part) => part.trim()))
    .filter((parts) => parts.some((part) => part.length > 0))
    .filter((parts) => parts.length >= 4)
    .filter((parts) => !parts.join('').includes('负责人'))
    .flatMap((parts) => {
      const workerField = parts[0]
      const dateField = parts[2]
      const durationField = parts[3]

      const normalizedDate = normalizeImportedDate(dateField)
      const duration = normalizeDuration(durationField)
      const workers = extractWorkers(workerField)

      return workers.map<WorkSession>((worker) => ({
        date: normalizedDate,
        workerName: worker,
        duration,
      }))
    })
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

function normalizeImportedDate(value: string) {
  const cleaned = value
    .replaceAll('年', '-')
    .replaceAll('月', '-')
    .replaceAll('日', '')
    .replaceAll('/', '-')
    .replaceAll('.', '-')
  const parts = cleaned.split('-').filter(Boolean)
  if (parts.length >= 3) {
    return dayjs(`${parts[0]}-${parts[1]}-${parts[2]}`).format('YYYY-MM-DD')
  }
  if (parts.length >= 2) {
    const now = dayjs()
    const month = Number(parts[0])
    const day = Number(parts[1])
    const year = month > now.month() + 3 ? now.year() - 1 : now.year()
    return dayjs(`${year}-${month}-${day}`).format('YYYY-MM-DD')
  }
  return dayjs().format('YYYY-MM-DD')
}

function normalizeDuration(value: string) {
  const parsed = Number(value.replace(/[^\d.]/g, ''))
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1
}

function extractWorkers(workerField: string) {
  const normalized = workerField.replace(/\r/g, '').trim()
  const mentions = Array.from(normalized.matchAll(/@([^\s@"]+)/g))
    .map((match) => match[1].trim())
    .filter(Boolean)

  if (mentions.length > 0) {
    return mentions
  }

  return normalized
    .replaceAll('"', '')
    .split(/\s+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function parseTabSeparatedRows(raw: string) {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let inQuotes = false

  for (let index = 0; index < raw.length; index += 1) {
    const char = raw[index]
    const nextChar = raw[index + 1]

    if (char === '"') {
      if (inQuotes && nextChar === '"') {
        field += '"'
        index += 1
      } else {
        inQuotes = !inQuotes
      }
      continue
    }

    if (!inQuotes && char === '\t') {
      row.push(field)
      field = ''
      continue
    }

    if (!inQuotes && (char === '\n' || char === '\r')) {
      if (char === '\r' && nextChar === '\n') {
        index += 1
      }
      row.push(field)
      rows.push(row)
      row = []
      field = ''
      continue
    }

    field += char
  }

  if (field.length > 0 || row.length > 0) {
    row.push(field)
    rows.push(row)
  }

  return rows
}
