export type Role = 'USER' | 'LEADER' | 'OWNER' | 'ADMIN' | 'HR' | 'FINANCE'
export type ViewMode = 'all' | 'single' | 'double'

export interface User {
  id: number
  username: string
  realName: string
  studentNumber: string
  role: Role
  isActive: boolean
  mustChangePassword: boolean
  createdAt?: string
  permissions: string[]
  semesterMember: boolean
  sortOrder: number
}

export interface SemesterSummary {
  id: string
  name: string
  database?: string
  archived: boolean
  draft: boolean
  active: boolean
  firstMonday: string
  contextVersion: number
  createdAt: string
  updatedAt: string
}

export interface LoginResponse {
  token: string
  refreshToken: string
  user: User
}

export interface ChangePasswordResponse {
  message: string
  user: User
  token: string
  refreshToken: string
}

export interface AvailabilityPayload {
  single: string[]
  double: string[]
}

export interface AvailabilityOverviewItem {
  username: string
  realName: string
  availability: AvailabilityPayload
}

export interface ScheduleResponse {
  schedule: Record<string, string[]>
  shiftDistribution: DashboardChartItem[]
}

export interface FinalScheduleResponse {
  weekNumber: number
  selectedDate: string
  isOddWeek: boolean
  source: 'saved' | 'generated'
  schedule: Record<string, string[]>
}

export interface WorkSession {
  id?: number
  date: string
  workerName: string
  duration: number
}

export interface WorkOrder {
  id: string
  title: string
  belongingMonth: string
  createdTime: string
  createdBy: string
  workSessions: WorkSession[]
}

export interface WorkOrderDraft {
  title: string
  belongingMonth: string
  workSessions: WorkSession[]
}

export interface DashboardChartItem {
  name: string
  value: number
}

export interface DashboardData {
  availabilityUserCount: number
  totalAssignedShifts: number
  workOrderCount: number
  schedule: Record<string, string[]>
  shiftDistribution: DashboardChartItem[]
  workDurationShare: DashboardChartItem[]
}

export interface FinanceWorkOrderDetail {
  title: string
  dates: string
  hours: number
  amount: number
}

export interface FinanceSummary {
  month: string
  startDate?: string
  endDate?: string
  dutyHours: number
  dutyAmount: number
  workOrderHours: number
  workOrderAmount: number
  managementAmount: number
  managementPending: boolean
  totalAmount: number
  workOrderDetails: FinanceWorkOrderDetail[]
}

export interface FinanceSaveLocalPayload {
  startDate: string
  endDate: string
  workOrderIds: string[]
  includeManagement: boolean
  managementMonths: number
}

export interface FinanceLocalBatch {
  id: string
  createdAt: string
  startDate: string
  endDate: string
  outputMonth: string
  workOrderIds: string[]
  includeManagement: boolean
  managementMonths: number
  excelFilename: string
  csvFilename: string
}

export interface MetaConfig {
  weekdaysCode: string[]
  weekdaysDisplay: string[]
  timeSlots: string[]
  userNames: string[]
  userRoles: Record<Role, string>
  firstMonday: string
  semester: SemesterSummary
}

export interface SystemSettings {
  firstMonday: string
  workStudyContent: string
  semester: SemesterSummary
  dutyRate: number
  workOrderRate: number
  mgmtLeaderRate: number
  mgmtOwnerRate: number
}

export interface UpdateSystemSettingsPayload {
  firstMonday: string
  workStudyContent: string
  dutyRate: number
  workOrderRate: number
  mgmtLeaderRate: number
  mgmtOwnerRate: number
}

export interface AuditLogItem {
  id: number
  createdAt: string
  username: string
  realName: string
  action: string
  status: number
  semesterId: string
  ip: string
}

export interface AuditLogListResponse {
  items: AuditLogItem[]
  total: number
  page: number
  pageSize: number
}

export interface AutoScheduleResponse {
  schedule: Record<string, string[]>
  shiftDistribution: DashboardChartItem[]
  warnings: string[]
}

export interface CreateSemesterPayload {
  name: string
  firstMonday: string
  cloneFromId: string
}

export interface CreateMemberPayload {
  username: string
  realName: string
  studentNumber: string
  role: Role
  initialPassword: string
}

export interface WorkStudyTemplateItem {
  filename: string
  exists: boolean
  size: number
  updatedAt?: string
}

export interface LaborConvertNoiseItem {
  name: string
  reduction: string
}

export interface LaborConvertNoise {
  applied: boolean
  items: LaborConvertNoiseItem[]
}

export interface LaborConvertSummary {
  originalTotal: string
  targetTotal: string
  finalTotal: string
  teamFund: string
  warnings: string[]
  noise: LaborConvertNoise
}

export interface LaborConvertRow {
  name: string
  original: string
  adjusted: string
  delta: string
  tax: string
  net: string
  remark: string
}

export interface LaborConvertTransfer {
  source: string
  receiver: string
  amount: string
}

export interface LaborConvertResult {
  historyId: string
  createdAt: string
  inputFilename: string
  outputName: string
  csvName?: string
  hasCsv: boolean
  csvOutputMonth?: string
  sourceFinanceBatchId?: string
  parentRunId?: string
  isManualAdjust: boolean
  canManualAdjust: boolean
  summary: LaborConvertSummary
  rows: LaborConvertRow[]
  transfers: LaborConvertTransfer[]
}

export interface LaborConvertHistoryItem {
  id: string
  createdAt: string
  inputFilename: string
  outputName: string
  csvName?: string
  csvOutputMonth?: string
  targetTotal: string
  finalTotal: string
  hasCsv: boolean
  canManualAdjust: boolean
  sourceFinanceBatchId?: string
  isManualAdjust: boolean
}

export interface LaborFinanceFileItem {
  id: string
  createdAt: string
  startDate: string
  endDate: string
  outputMonth: string
  excelFilename: string
}
