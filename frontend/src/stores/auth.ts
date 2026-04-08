import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { changePassword, fetchMe, login } from '@/api/services'
import type { User } from '@/types'

const TOKEN_KEY = 'pms_token'
const USER_KEY = 'pms_user'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) || '')
  const user = ref<User | null>(readStoredUser())
  const isAuthenticated = computed(() => Boolean(token.value))

  function setSession(nextToken: string, nextUser: User) {
    token.value = nextToken
    user.value = nextUser
    localStorage.setItem(TOKEN_KEY, nextToken)
    localStorage.setItem(USER_KEY, JSON.stringify(nextUser))
  }

  function hydrate() {
    token.value = localStorage.getItem(TOKEN_KEY) || ''
    user.value = readStoredUser()
  }

  async function loginWithPassword(payload: { username: string; password: string }) {
    const response = await login(payload)
    setSession(response.token, response.user)
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
    user.value = response.user
    localStorage.setItem(USER_KEY, JSON.stringify(response.user))
    return response
  }

  function can(permission: string) {
    return user.value?.permissions?.includes(permission) ?? false
  }

  function hasRole(roles: string[]) {
    return roles.includes(user.value?.role || '')
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  return {
    token,
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
