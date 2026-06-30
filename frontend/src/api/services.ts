import { apiClient } from './client'
import type {
  AvailabilityOverviewItem,
  AvailabilityPayload,
  DashboardData,
  FinanceLocalBatch,
  FinanceSaveLocalPayload,
  FinanceSummary,
  FinalScheduleResponse,
  LaborConvertHistoryItem,
  LaborConvertResult,
  LaborFinanceFileItem,
  LoginResponse,
  MetaConfig,
  ScheduleResponse,
  SystemSettings,
  UpdateSystemSettingsPayload,
  User,
  WorkOrder,
  WorkOrderDraft,
} from '@/types'

export async function login(payload: { username: string; password: string }) {
  const { data } = await apiClient.post<LoginResponse>('/auth/login', payload)
  return data
}

export async function fetchMe() {
  const { data } = await apiClient.get<User>('/auth/me')
  return data
}

export async function changePassword(payload: { currentPassword: string; newPassword: string }) {
  const { data } = await apiClient.put<{ message: string; user: User }>('/auth/password', payload)
  return data
}

export async function fetchMetaConfig() {
  const { data } = await apiClient.get<MetaConfig>('/meta/config')
  return data
}

export async function fetchDashboard() {
  const { data } = await apiClient.get<DashboardData>('/dashboard')
  return data
}

export async function fetchFinanceSummary(
  payload:
    | string
    | {
        month?: string
        realName?: string
        startDate?: string
        endDate?: string
        workOrderIds?: string[]
        includeManagement?: boolean
        managementMonths?: number
      },
  realName = '',
) {
  if (typeof payload !== 'string') {
    const { data } = await apiClient.get<FinanceSummary>('/finance', {
      params: {
        month: payload.month,
        realName: payload.realName || '',
        startDate: payload.startDate,
        endDate: payload.endDate,
        workOrderIds: (payload.workOrderIds || []).join(','),
        includeManagement: payload.includeManagement,
        managementMonths: payload.managementMonths,
      },
    })
    return data
  }

  const { data } = await apiClient.get<FinanceSummary>('/finance', {
    params: { month: payload, realName },
  })
  return data
}

export async function fetchAvailabilityOverview() {
  const { data } = await apiClient.get<{ items: AvailabilityOverviewItem[] }>('/availability')
  return data.items
}

export async function fetchMyAvailability() {
  const { data } = await apiClient.get<AvailabilityPayload>('/availability/me')
  return data
}

export async function saveMyAvailability(payload: AvailabilityPayload) {
  const { data } = await apiClient.put<{ message: string }>('/availability/me', payload)
  return data
}

export async function fetchUserAvailability(username: string) {
  const { data } = await apiClient.get<AvailabilityPayload>(`/availability/users/${username}`)
  return data
}

export async function saveUserAvailability(username: string, payload: AvailabilityPayload) {
  const { data } = await apiClient.put<{ message: string }>(`/availability/users/${username}`, payload)
  return data
}

export async function fetchSchedule() {
  const { data } = await apiClient.get<ScheduleResponse>('/schedule')
  return data.schedule
}

export async function fetchScheduleSummary() {
  const { data } = await apiClient.get<ScheduleResponse>('/schedule')
  return data
}

export async function saveSchedule(schedule: Record<string, string[]>) {
  const { data } = await apiClient.put<{ message: string }>('/schedule', { schedule })
  return data
}

export async function fetchFinalSchedule(weekNumber: number, date: string) {
  const { data } = await apiClient.get<FinalScheduleResponse>(`/final-schedules/${weekNumber}`, {
    params: { date },
  })
  return data
}

export async function saveFinalSchedule(
  weekNumber: number,
  payload: { selectedDate: string; schedule: Record<string, string[]> },
) {
  const { data } = await apiClient.put<{ message: string }>(`/final-schedules/${weekNumber}`, payload)
  return data
}

export async function fetchWorkOrders(month: string) {
  const { data } = await apiClient.get<{ items: WorkOrder[] }>('/work-orders', {
    params: { month },
  })
  return data.items
}

export async function createWorkOrder(payload: WorkOrderDraft) {
  const { data } = await apiClient.post<WorkOrder>('/work-orders', payload)
  return data
}

export async function updateWorkOrder(id: string, payload: WorkOrderDraft) {
  const { data } = await apiClient.put<WorkOrder>(`/work-orders/${id}`, payload)
  return data
}

export async function deleteWorkOrder(id: string) {
  const { data } = await apiClient.delete<{ message: string }>(`/work-orders/${id}`)
  return data
}

export async function fetchUsers() {
  const { data } = await apiClient.get<{ items: User[] }>('/users')
  return data.items
}

export async function fetchSystemSettings() {
  const { data } = await apiClient.get<SystemSettings>('/system-settings')
  return data
}

export async function updateSystemSettings(payload: UpdateSystemSettingsPayload) {
  const { data } = await apiClient.put<{ message: string }>('/system-settings', payload)
  return data
}

export async function updateUserRole(id: number, role: string) {
  const { data } = await apiClient.patch<{ message: string }>(`/users/${id}/role`, { role })
  return data
}

export async function updateUserStatus(id: number, isActive: boolean) {
  const { data } = await apiClient.patch<{ message: string }>(`/users/${id}/status`, { isActive })
  return data
}

export async function resetUserPassword(id: number, newPassword: string) {
  const { data } = await apiClient.patch<{ message: string }>(`/users/${id}/password`, { newPassword })
  return data
}

export async function convertLabor(payload: FormData) {
  const { data } = await apiClient.post<LaborConvertResult>('/labor-convert', payload, {
    timeout: 60000,
  })
  return data
}

export async function fetchLaborConvertHistory() {
  const { data } = await apiClient.get<{ items: LaborConvertHistoryItem[] }>('/labor-convert/history')
  return data.items
}

export async function fetchLaborFinanceFiles() {
  const { data } = await apiClient.get<{ items: LaborFinanceFileItem[] }>('/labor-convert/finance-files')
  return data.items
}

export async function convertLaborFromFinance(payload: { batchId: string; targetTotal: string; seed?: string }) {
  const { data } = await apiClient.post<LaborConvertResult>('/labor-convert/from-finance', payload, {
    timeout: 60000,
  })
  return data
}

export async function fetchLaborConvertHistoryDetail(id: string) {
  const { data } = await apiClient.get<LaborConvertResult>(`/labor-convert/history/${id}`)
  return data
}

export async function downloadLaborConvertWorkbook(id: string) {
  const response = await apiClient.get(`/labor-convert/history/${id}/download`, {
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function downloadLaborConvertRecords(id: string) {
  const response = await apiClient.get(`/labor-convert/history/${id}/download/records`, {
    responseType: 'blob',
    timeout: 60000,
  })
  return response.data as Blob
}

export async function saveLaborManualAdjustment(
  id: string,
  payload: { rows: { name: string; adjusted: string }[] },
) {
  const { data } = await apiClient.post<LaborConvertResult>(`/labor-convert/history/${id}/manual-adjust`, payload, {
    timeout: 60000,
  })
  return data
}

export async function downloadScheduleWorkbook() {
  const response = await apiClient.get('/schedule/export', {
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function downloadWorkOrderWorkbook(month: string) {
  const response = await apiClient.get('/work-orders/export', {
    params: { month },
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function downloadFinanceWorkbook(
  payload: string | {
    startDate: string
    endDate: string
    workOrderIds: string[]
    includeManagement: boolean
    managementMonths: number
  },
) {
  if (typeof payload !== 'string') {
    const response = await apiClient.get('/finance/export', {
      params: {
        startDate: payload.startDate,
        endDate: payload.endDate,
        workOrderIds: payload.workOrderIds.join(','),
        includeManagement: payload.includeManagement,
        managementMonths: payload.managementMonths,
      },
      responseType: 'blob',
    })
    return response.data as Blob
  }

  const response = await apiClient.get('/finance/export', {
    params: { month: payload },
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function downloadDutyCSV(payload: {
  startDate: string
  endDate: string
  outputMonth: string
  workOrderIds: string[]
  includeManagement: boolean
  managementMonths: number
}) {
  const response = await apiClient.get('/finance/duty-csv', {
    params: {
      startDate: payload.startDate,
      endDate: payload.endDate,
      outputMonth: payload.outputMonth,
      workOrderIds: payload.workOrderIds.join(','),
      includeManagement: payload.includeManagement,
      managementMonths: payload.managementMonths,
    },
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function saveFinanceExportsLocal(payload: FinanceSaveLocalPayload) {
  const { data } = await apiClient.post<{ message: string; batch: FinanceLocalBatch }>('/finance/save-local', payload, {
    timeout: 60000,
  })
  return data
}
