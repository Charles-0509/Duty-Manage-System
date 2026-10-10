import { apiClient } from './client'
import type { MonthlyConditions, MonthlySummary, MonthlyView, MonthlyVersionRequest } from '@/types/monthly'

export async function fetchMonthlyReports() {
  const { data } = await apiClient.get<{ items: MonthlySummary[] }>('/monthly-reports')
  return data.items
}

export async function previewMonthlyReport(conditions: MonthlyConditions) {
  const { data } = await apiClient.post<MonthlyView>('/monthly-reports/preview', conditions)
  return data
}

export async function createMonthlyReport(payload: MonthlyConditions & { targetTotal: string; proxyMemberIds: number[]; sourceHash: string }) {
  const { data } = await apiClient.post<MonthlyView>('/monthly-reports', payload)
  return data
}

export async function fetchMonthlyReport(id: string, versionId?: string) {
  const { data } = await apiClient.get<MonthlyView>(`/monthly-reports/${id}`, { params: { versionId } })
  return data
}

export async function newMonthlyVersion(id: string, payload: MonthlyVersionRequest) {
  const { data } = await apiClient.post<MonthlyView>(`/monthly-reports/${id}/versions`, payload)
  return data
}

export async function checkMonthlyReport(id: string, versionId: string) {
  const { data } = await apiClient.post<MonthlyView>(`/monthly-reports/${id}/check`, { versionId })
  return data
}

export async function finalizeMonthlyReport(id: string, versionId: string, confirmHighAmounts: boolean) {
  const { data } = await apiClient.post<MonthlyView>(`/monthly-reports/${id}/finalize`, { versionId, confirmHighAmounts })
  return data
}

export async function generateMonthlyReport(id: string, versionId: string) {
	const { data } = await apiClient.post<MonthlyView>(`/monthly-reports/${id}/generate`, { versionId }, { timeout: 120000 })
	return data
}

export async function deleteMonthlyDraft(id: string, versionId: string) {
  await apiClient.delete(`/monthly-reports/${id}/versions/${versionId}`)
}

export async function downloadMonthlyFile(id: string, versionId: string, kind: string, source = false) {
  const path = source ? `sources/${versionId}` : `versions/${versionId}`
  const { data, headers } = await apiClient.get<Blob>(`/monthly-reports/${id}/${path}/download/${kind}`, { responseType: 'blob', timeout: 120000 })
  const disposition = String(headers['content-disposition'] || '')
  const encoded = /filename\*=UTF-8''([^;]+)/i.exec(disposition)?.[1]
  const plain = /filename="([^"]+)"/.exec(disposition)?.[1]
  return { blob: data, filename: (encoded ? decodeURIComponent(encoded) : plain || `${kind}.zip`).split('/').pop()! }
}
