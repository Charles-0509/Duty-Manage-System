import { apiClient } from './client'
import type {
  AvailabilityOverviewItem,
  AvailabilityPayload,
  DashboardData,
  FinanceSummary,
  FinalScheduleResponse,
  LoginResponse,
  MetaConfig,
  ScheduleResponse,
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

export async function fetchFinanceSummary(month: string, realName = '') {
  const { data } = await apiClient.get<FinanceSummary>('/finance', {
    params: { month, realName },
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

export async function downloadFinanceWorkbook(month: string) {
  const response = await apiClient.get('/finance/export', {
    params: { month },
    responseType: 'blob',
  })
  return response.data as Blob
}
