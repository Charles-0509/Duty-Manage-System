import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { changePassword, fetchMe, login, logout as logoutApi } from '@/api/services'
import { REFRESH_TOKEN_KEY, TOKEN_KEY, USER_KEY } from '@/api/client'
import type { User } from '@/types'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) || '')
  const refreshToken = ref<string>(localStorage.getItem(REFRESH_TOKEN_KEY) || '')
  const user = ref<User | null>(readStoredUser())
  const isAuthenticated = computed(() => Boolean(token.value))

  function setSession(nextToken: string, nextRefreshToken: string, nextUser: User) {
    token.value = nextToken
    refreshToken.value = nextRefreshToken
    user.value = nextUser
    localStorage.setItem(TOKEN_KEY, nextToken)
    localStorage.setItem(REFRESH_TOKEN_KEY, nextRefreshToken)
    localStorage.setItem(USER_KEY, JSON.stringify(nextUser))
  }

  function hydrate() {
    token.value = localStorage.getItem(TOKEN_KEY) || ''
    refreshToken.value = localStorage.getItem(REFRESH_TOKEN_KEY) || ''
    user.value = readStoredUser()
  }

  async function loginWithPassword(payload: { username: string; password: string }) {
    const response = await login(payload)
    setSession(response.token, response.refreshToken, response.user)
    return response
  }

  async function refreshMe() {
    if (!token.value) return null
    const profile = await fetchMe()
    user.value = profile
    localStorage.setItem(USER_KEY, JSON.stringify(profile))
    return profile
  }

  async function changeOwnPassword(payload: { currentPassword: string; newPassword: string }) {
    const response = await changePassword(payload)
    // The backend invalidated all old tokens; adopt the fresh pair it returned.
    setSession(response.token, response.refreshToken, response.user)
    return response
  }

  function can(permission: string) {
    return user.value?.permissions?.includes(permission) ?? false
  }

  function hasRole(roles: string[]) {
    return roles.includes(user.value?.role || '')
  }

  function logout() {
    const currentRefreshToken = refreshToken.value
    token.value = ''
    refreshToken.value = ''
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
    if (currentRefreshToken) {
      void logoutApi(currentRefreshToken)
    }
  }

  return {
    token,
    refreshToken,
    user,
    isAuthenticated,
    hydrate,
    loginWithPassword,
    refreshMe,
    changeOwnPassword,
    can,
    hasRole,
    setSession,
    logout,
  }
})

function readStoredUser() {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as User
  } catch {
    return null
  }
}
