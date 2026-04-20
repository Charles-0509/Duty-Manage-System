export type Role = 'USER' | 'LEADER' | 'OWNER' | 'ADMIN' | 'HR'
export type ViewMode = 'all' | 'single' | 'double'

export interface User {
  id: number
  username: string
  realName: string
  role: Role
  isActive: boolean
  mustChangePassword: boolean
  createdAt?: string
  permissions: string[]
}

export interface LoginResponse {
  token: string
  user: User
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
  dutyHours: number
  dutyAmount: number
  workOrderHours: number
  workOrderAmount: number
  managementAmount: number
  managementPending: boolean
  totalAmount: number
  workOrderDetails: FinanceWorkOrderDetail[]
}

export interface MetaConfig {
  weekdaysCode: string[]
  weekdaysDisplay: string[]
  timeSlots: string[]
  userNames: string[]
  userRoles: Record<Role, string>
  rolePermissions: Record<Role, string[]>
  firstMonday: string
}

