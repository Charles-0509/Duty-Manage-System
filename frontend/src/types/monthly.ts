export interface MonthlyConditions {
  outputMonth: string
  startDate: string
  endDate: string
  workOrderIds: string[]
  includeManagement: boolean
  managementMonths: number
}

export interface MonthlySummary {
  id: string
  month: string
  finalVersionId: string
  latestVersionId: string
  createdAt: string
  updatedAt: string
}

export interface MonthlyVersion {
  id: string
  number: number
  status: 'draft' | 'finalized' | 'legacy'
  createdAt: string
  targetTotal: string
  originalTotal: string
  declaredTotal: string
  legacyNote?: string
}

export interface MonthlyRow {
  memberId: number
  name: string
  studentNumber: string
  role: string
  dutyHours: number
  workHours: number
  dutyAmount: string
  workAmount: string
  management: string
  original: string
  declared: string
  eligible: boolean
  salaryOutgoing: string
  salaryIncoming: string
  salaryOutstanding: string
  teamFund: string
}

export interface MonthlySource {
  id: string
  startDate: string
  endDate: string
  createdAt: string
  legacy: boolean
  hasExcel: boolean
  hasCsv: boolean
}

export interface MonthlyView {
  report: MonthlySummary
  version: MonthlyVersion
  versions: MonthlyVersion[]
  conditions: MonthlyConditions
  rows: MonthlyRow[]
  transfers: { sourceName: string; receiverName: string; kind: string; amount: string }[]
  originalTotal: string
  targetTotal: string
  declaredTotal: string
  teamFund: string
  unallocated: string
  sourceHash: string
  issues: string[]
  notes: string[]
  files: string[]
  templateHash: string
  sources: MonthlySource[]
}

export interface MonthlyVersionRequest {
  baseVersionId: string
  mode: 'copy' | 'manual' | 'allocate' | 'refresh'
  targetTotal?: string
  proxyMemberIds?: number[]
  rows?: { memberId: number; amount: string }[]
  conditions?: MonthlyConditions
  confirmHighAmounts?: boolean
}
