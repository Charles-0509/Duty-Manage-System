import axios from 'axios'

const baseURL = import.meta.env.VITE_API_BASE_URL || '/api'

export const TOKEN_KEY = 'pms_token'
export const REFRESH_TOKEN_KEY = 'pms_refresh_token'
export const USER_KEY = 'pms_user'

export const apiClient = axios.create({
  baseURL,
  timeout: 15000,
})

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

let semesterReloadScheduled = false

// --- Access-token refresh with single-flight --------------------------------
// When the short-lived access token expires, concurrent requests share one
// /auth/refresh call and are replayed with the new token.
let refreshPromise: Promise<boolean> | null = null

async function performRefresh(): Promise<boolean> {
  const refreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
  if (!refreshToken) return false
  try {
    // Use a bare axios call so the refresh itself never re-enters this
    // interceptor.
    const { data } = await axios.post(
      `${baseURL}/auth/refresh`,
      { refreshToken },
      { timeout: 15000 },
    )
    localStorage.setItem(TOKEN_KEY, data.token)
    localStorage.setItem(REFRESH_TOKEN_KEY, data.refreshToken)
    localStorage.setItem(USER_KEY, JSON.stringify(data.user))
    return true
  } catch {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
    return false
  }
}

function refreshSession(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = performRefresh().finally(() => {
      refreshPromise = null
    })
  }
  return refreshPromise
}

function redirectToLogin() {
  if (!window.location.hash.includes('/login')) {
    window.location.hash = '#/login'
  }
}

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
        localStorage.removeItem(USER_KEY)
        window.setTimeout(() => window.location.reload(), 80)
      }
    }
    return response
  },
  async (error) => {
    const config = error?.config as (typeof error.config & { _retried?: boolean }) | undefined
    const status = error?.response?.status
    const isAuthRefreshCall = config?.url?.includes('/auth/refresh')

    if (status === 401 && config && !config._retried && !isAuthRefreshCall) {
      config._retried = true
      if (await refreshSession()) {
        const token = localStorage.getItem(TOKEN_KEY)
        if (token) {
          config.headers = config.headers ?? {}
          config.headers.Authorization = `Bearer ${token}`
          return apiClient.request(config)
        }
      }
      redirectToLogin()
      return Promise.reject(error)
    }

    if (status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem(REFRESH_TOKEN_KEY)
      localStorage.removeItem(USER_KEY)
      redirectToLogin()
    }
    return Promise.reject(error)
  },
)
