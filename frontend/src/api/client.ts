import axios from 'axios'
import { ElMessage } from 'element-plus'

const baseURL = '/api'

export const TOKEN_KEY = 'pms_token'
export const REFRESH_TOKEN_KEY = 'pms_refresh_token'
export const USER_KEY = 'pms_user'
export const DEVICE_ID_KEY = 'dms_device_id'

function loginDeviceID() {
  let deviceID = localStorage.getItem(DEVICE_ID_KEY)
  if (!deviceID) {
    deviceID = globalThis.crypto.randomUUID()
    localStorage.setItem(DEVICE_ID_KEY, deviceID)
  }
  return deviceID
}

export const apiClient = axios.create({
  baseURL,
  timeout: 15000,
})

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  if (config.url?.includes('/auth/login')) {
    config.headers['X-DMS-Device-ID'] = loginDeviceID()
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
  } catch (error: any) {
    // Only a definitive rejection from the refresh endpoint means the session
    // is dead. Network errors / timeouts must not wipe the stored session,
    // otherwise a flaky connection would log the user out.
    if (error?.response?.status === 401) {
      clearStoredSession()
    }
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

function clearStoredSession() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

function redirectToLogin() {
  if (!window.location.hash.includes('/login')) {
    window.location.hash = '#/login'
  }
}

// Parallel requests that all hit an expired session should produce exactly
// one notice, not one toast per failed request.
let lastAuthExpiredNoticeAt = 0

function handleSessionExpired(error: unknown): Promise<never> {
  const now = Date.now()
  if (now - lastAuthExpiredNoticeAt > 3000) {
    lastAuthExpiredNoticeAt = now
    window.setTimeout(() => {
      ElMessage.closeAll()
      ElMessage.warning('登录状态已过期，请重新登录')
    }, 0)
  }
  clearStoredSession()
  redirectToLogin()
  return Promise.reject(error)
}

apiClient.interceptors.response.use(
  (response) => {
    const semesterId = response.headers['x-dms-semester-id'] as string | undefined
    if (semesterId) {
      const contextVersion = String(response.headers['x-dms-context-version'] || '')
      const context = `${semesterId}:${contextVersion}`
      const previous = localStorage.getItem('dms_semester_context')
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
    // /auth/login 的 401 表示用户名或密码错误，由登录页自己提示；
    // 会话过期处理只针对携带令牌的业务请求。
    const isAuthEndpoint = config?.url?.includes('/auth/login') || config?.url?.includes('/auth/refresh')

    if (status === 401 && config && !config._retried && !isAuthEndpoint) {
      config._retried = true
      const refreshed = await refreshSession()
      if (refreshed) {
        const token = localStorage.getItem(TOKEN_KEY)
        if (token) {
          config.headers = config.headers ?? {}
          config.headers.Authorization = `Bearer ${token}`
          return apiClient.request(config)
        }
      }
      if (!localStorage.getItem(REFRESH_TOKEN_KEY)) {
        // Refresh token was rejected: the session is gone for good.
        return handleSessionExpired(error)
      }
      // Refresh could not complete (network error) — surface the original
      // failure so pages can show their own error message.
      return Promise.reject(error)
    }

    if (status === 401 && !isAuthEndpoint) {
      return handleSessionExpired(error)
    }
    return Promise.reject(error)
  },
)
