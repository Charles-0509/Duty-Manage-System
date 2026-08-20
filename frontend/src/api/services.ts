import { apiClient } from './client'
import type {
  AuditLogListResponse,
  AutoScheduleResponse,
  AvailabilityOverviewItem,
  AvailabilityPayload,
  ChangePasswordResponse,
  CreateMemberPayload,
  CreateSemesterPayload,
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
  SchedulePlanResponse,
  SchedulePlanSummary,
  ScheduleResponse,
  SemesterSummary,
  SystemSettings,
  UpdateSystemSettingsPayload,
  User,
  WorkStudyTemplateItem,
  WorkOrder,
  WorkOrderDraft,
  WorkOrderListResponse,
} from '@/types'

export async function login(payload: { username: string; password: string }) {
  const { data } = await apiClient.post<LoginResponse>('/auth/login', payload)
  return data
}

export async function fetchMe() {
  const { data } = await apiClient.get<User>('/auth/me')
  return data
}

export async function logout(refreshToken: string) {
  try {
    await apiClient.post('/auth/logout', { refreshToken })
  } catch {
    // Signing out is best-effort; clear local state regardless.
  }
}

export async function changePassword(payload: { currentPassword: string; newPassword: string }) {
  const { data } = await apiClient.put<ChangePasswordResponse>('/auth/password', payload)
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
  payload: {
    realName: string
    startDate: string
    endDate: string
    workOrderIds: string[]
    includeManagement: boolean
    managementMonths: number
  },
) {
  const { data } = await apiClient.get<FinanceSummary>('/finance', {
    params: {
      realName: payload.realName,
      startDate: payload.startDate,
      endDate: payload.endDate,
      workOrderIds: payload.workOrderIds.join(','),
      includeManagement: payload.includeManagement,
      managementMonths: payload.managementMonths,
    },
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
  return (await fetchScheduleSummary()).schedule
}

export async function fetchScheduleSummary() {
  const { data } = await apiClient.get<ScheduleResponse>('/schedule')
  return data
}

export async function fetchSchedulePlans() {
  const { data } = await apiClient.get<{ items: SchedulePlanSummary[] }>('/schedule-plans')
  return data.items
}

export async function fetchSchedulePlan(id: string) {
  const { data } = await apiClient.get<SchedulePlanResponse>(`/schedule-plans/${id}`)
  return data
}

export async function createSchedulePlan(name: string, schedule: Record<string, string[]>) {
  const { data } = await apiClient.post<SchedulePlanSummary>('/schedule-plans', { name, schedule })
  return data
}

export async function updateSchedulePlan(id: string, name: string, schedule: Record<string, string[]>) {
  const { data } = await apiClient.put<SchedulePlanSummary>(`/schedule-plans/${id}`, { name, schedule })
  return data
}

export async function renameSchedulePlan(id: string, name: string) {
  const { data } = await apiClient.patch<SchedulePlanSummary>(`/schedule-plans/${id}`, { name })
  return data
}

export async function deleteSchedulePlan(id: string) {
  const { data } = await apiClient.delete<{ message: string }>(`/schedule-plans/${id}`)
  return data
}

export async function publishSchedulePlan(id: string) {
  const { data } = await apiClient.post<SchedulePlanSummary>(`/schedule-plans/${id}/publish`)
  return data
}

export async function importSchedulePlan(file: File, name: string) {
  const form = new FormData()
  form.append('file', file)
  form.append('name', name)
  const { data } = await apiClient.post<SchedulePlanSummary>('/schedule-plans/import', form)
  return data
}

export async function autoGenerateSchedule(perSlot: number, schedule: Record<string, string[]>) {
  const { data } = await apiClient.post<AutoScheduleResponse>('/schedule/auto-generate', { perSlot, schedule })
  return data
}

export async function fetchAuditLogs(page: number, pageSize: number, username = '') {
  const { data } = await apiClient.get<AuditLogListResponse>('/audit-logs', {
    params: { page, pageSize, username },
  })
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

export async function fetchWorkOrderPage(month: string, page: number, pageSize = 20) {
  const { data } = await apiClient.get<WorkOrderListResponse>('/work-orders', {
    params: { month, page, pageSize },
  })
  return data
}

export async function fetchWorkOrders(month: string) {
  const items: WorkOrder[] = []
  let page = 1
  while (true) {
    const result = await fetchWorkOrderPage(month, page, 100)
    items.push(...result.items)
    if (items.length >= result.total || result.items.length === 0) return items
    page++
  }
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

export async function fetchSemesters() {
  const { data } = await apiClient.get<{ items: SemesterSummary[]; active: SemesterSummary }>('/semesters')
  return data
}

export async function createSemester(payload: CreateSemesterPayload) {
  const { data } = await apiClient.post<SemesterSummary>('/semesters', payload)
  return data
}

export async function activateSemester(id: string) {
  const { data } = await apiClient.post<SemesterSummary>(`/semesters/${id}/activate`)
  return data
}

export async function setSemesterArchived(id: string, archived: boolean) {
  const action = archived ? 'archive' : 'unarchive'
  const { data } = await apiClient.post<{ message: string }>(`/semesters/${id}/${action}`)
  return data
}

export async function renameSemester(id: string, name: string) {
  const { data } = await apiClient.patch<{ message: string }>(`/semesters/${id}`, { name })
  return data
}

export async function deleteSemester(id: string) {
  const { data } = await apiClient.delete<{ message: string }>(`/semesters/${id}`)
  return data
}

export async function exportSemester(id: string) {
  const response = await apiClient.get(`/semesters/${id}/export`, { responseType: 'blob', timeout: 60000 })
  return response.data as Blob
}

export async function importSemester(file: File) {
  const payload = new FormData()
  payload.append('file', file)
  const { data } = await apiClient.post<SemesterSummary>('/semesters/import', payload, { timeout: 60000 })
  return data
}

export async function createUser(payload: CreateMemberPayload) {
  const { data } = await apiClient.post<{ message: string }>('/users', payload)
  return data
}

export async function updateUserProfile(id: number, payload: { realName: string; studentNumber: string; role: string; sortOrder: number }) {
  const { data } = await apiClient.patch<{ message: string }>(`/users/${id}/profile`, payload)
  return data
}

export async function removeUserMembership(id: number) {
  const { data } = await apiClient.delete<{ message: string }>(`/users/${id}/membership`)
  return data
}

export async function restoreUserMembership(id: number) {
  const { data } = await apiClient.patch<{ message: string }>(`/users/${id}/membership`)
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

export async function fetchWorkStudyTemplate() {
  const { data } = await apiClient.get<WorkStudyTemplateItem>('/templates/global')
  return data
}

export async function uploadWorkStudyTemplate(file: File) {
  const payload = new FormData()
  payload.append('file', file)
  const { data } = await apiClient.put<WorkStudyTemplateItem>('/templates/global', payload, { timeout: 60000 })
  return data
}

export async function downloadWorkStudyTemplate() {
  const response = await apiClient.get('/templates/global/download', { responseType: 'blob' })
  return response.data as Blob
}

export async function deleteWorkStudyTemplate() {
  const { data } = await apiClient.delete<{ message: string }>('/templates/global')
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

export async function deleteFinanceLocalBatch(id: string) {
  const { data } = await apiClient.delete<{ message: string }>(`/labor-convert/finance-files/${id}`)
  return data
}

export async function convertLaborFromFinance(payload: { batchId: string; targetTotal: string }) {
  const { data } = await apiClient.post<LaborConvertResult>('/labor-convert/from-finance', payload, {
    timeout: 60000,
  })
  return data
}

export async function fetchLaborConvertHistoryDetail(id: string) {
  const { data } = await apiClient.get<LaborConvertResult>(`/labor-convert/history/${id}`)
  return data
}

export async function deleteLaborConvertHistory(id: string) {
  const { data } = await apiClient.delete<{ message: string }>(`/labor-convert/history/${id}`)
  return data
}

export async function downloadLaborConvertWorkbook(id: string) {
  const response = await apiClient.get(`/labor-convert/history/${id}/download`, {
    responseType: 'blob',
  })
  return response.data as Blob
}

export async function downloadLaborWorkStudyConversionWorkbook(id: string) {
  const response = await apiClient.get(`/labor-convert/history/${id}/download/work-study`, {
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

export async function downloadSchedulePlanWorkbook(id: string) {
  const response = await apiClient.get(`/schedule-plans/${id}/export`, {
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
  payload: {
    startDate: string
    endDate: string
    workOrderIds: string[]
    includeManagement: boolean
    managementMonths: number
  },
) {
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
