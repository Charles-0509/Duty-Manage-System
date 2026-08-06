import axios from 'axios'

const baseURL = import.meta.env.VITE_API_BASE_URL || '/api'

export const apiClient = axios.create({
  baseURL,
  timeout: 15000,
})

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('pms_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let semesterReloadScheduled = false

apiClient.interceptors.response.use(
  (response) => {
    const semesterId = response.headers['x-dms-semester-id'] as string | undefined
    if (semesterId) {
      const contextVersion = String(response.headers['x-dms-context-version'] || '')
      const context = `${semesterId}:${contextVersion}`
      const previous = localStorage.getItem('dms_semester_context')
      localStorage.setItem('dms_semester_id', semesterId)
      localStorage.setItem('dms_semester_context', context)
      if (previous && previous !== context && !semesterReloadScheduled) {
        semesterReloadScheduled = true
        localStorage.removeItem('pms_user')
        window.setTimeout(() => window.location.reload(), 80)
      }
    }
    return response
  },
  (error) => {
    if (error?.response?.status === 401) {
      localStorage.removeItem('pms_token')
      localStorage.removeItem('pms_user')
      if (!window.location.hash.includes('/login')) {
        window.location.hash = '#/login'
      }
    }
    return Promise.reject(error)
  },
)
