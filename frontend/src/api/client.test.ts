// @vitest-environment jsdom

import axios, { AxiosError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('element-plus', () => ({
  ElMessage: {
    closeAll: vi.fn(),
    warning: vi.fn(),
  },
}))

import { apiClient, REFRESH_TOKEN_KEY, TOKEN_KEY, USER_KEY } from './client'

const storedValues = new Map<string, string>()
const localStorageMock: Storage = {
  get length() {
    return storedValues.size
  },
  clear: () => storedValues.clear(),
  getItem: (key) => storedValues.get(key) ?? null,
  key: (index) => [...storedValues.keys()][index] ?? null,
  removeItem: (key) => storedValues.delete(key),
  setItem: (key, value) => storedValues.set(key, String(value)),
}
vi.stubGlobal('localStorage', localStorageMock)

function unauthorizedAdapter(config: InternalAxiosRequestConfig): Promise<AxiosResponse> {
  const response: AxiosResponse = {
    data: { message: 'unauthorized' },
    status: 401,
    statusText: 'Unauthorized',
    headers: {},
    config,
  }
  return Promise.reject(new AxiosError('unauthorized', 'ERR_BAD_REQUEST', config, undefined, response))
}

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  window.location.hash = ''
})

describe('expired session recovery', () => {
  it('clears an incomplete session and settles the failed request when no refresh token exists', async () => {
    localStorage.setItem(TOKEN_KEY, 'legacy-access-token')
    localStorage.setItem(USER_KEY, JSON.stringify({ username: 'admin' }))
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    window.location.hash = '#/dashboard'

    const requestResult = apiClient
      .get('/meta/config', { adapter: unauthorizedAdapter })
      .then(() => 'resolved', () => 'rejected')

    const result = await Promise.race([
      requestResult,
      new Promise<'timeout'>((resolve) => window.setTimeout(() => resolve('timeout'), 100)),
    ])

    expect(result).toBe('rejected')
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull()
    expect(localStorage.getItem(USER_KEY)).toBeNull()
    expect(window.location.hash).toBe('#/login')
  })

  it('clears the session when the refresh token is rejected', async () => {
    localStorage.setItem(TOKEN_KEY, 'expired-access-token')
    localStorage.setItem(REFRESH_TOKEN_KEY, 'rejected-refresh-token')
    localStorage.setItem(USER_KEY, JSON.stringify({ username: 'admin' }))
    window.location.hash = '#/dashboard'
    vi.spyOn(axios, 'post').mockRejectedValue({ response: { status: 401 } })

    await expect(apiClient.get('/meta/config', { adapter: unauthorizedAdapter })).rejects.toBeTruthy()

    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBeNull()
    expect(localStorage.getItem(USER_KEY)).toBeNull()
    expect(window.location.hash).toBe('#/login')
  })

  it('preserves the stored session when refresh fails because of a network error', async () => {
    localStorage.setItem(TOKEN_KEY, 'expired-access-token')
    localStorage.setItem(REFRESH_TOKEN_KEY, 'still-valid-refresh-token')
    localStorage.setItem(USER_KEY, JSON.stringify({ username: 'admin' }))
    window.location.hash = '#/dashboard'
    vi.spyOn(axios, 'post').mockRejectedValue(new AxiosError('network unavailable'))

    await expect(apiClient.get('/meta/config', { adapter: unauthorizedAdapter })).rejects.toBeTruthy()

    expect(localStorage.getItem(TOKEN_KEY)).toBe('expired-access-token')
    expect(localStorage.getItem(REFRESH_TOKEN_KEY)).toBe('still-valid-refresh-token')
    expect(localStorage.getItem(USER_KEY)).not.toBeNull()
    expect(window.location.hash).toBe('#/dashboard')
  })
})
