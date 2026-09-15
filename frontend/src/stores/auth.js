import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import api from '../api/client'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('mmg_token') || '')
  const username = ref(localStorage.getItem('mmg_user') || '')
  const role = ref(localStorage.getItem('mmg_role') || '')

  const isLoggedIn = computed(() => !!token.value)
  const canWrite = computed(() => role.value === 'engineer')

  async function login(u, p) {
    const { data } = await api.post('/auth/login', { username: u, password: p })
    token.value = data.token
    username.value = data.username
    role.value = data.role
    localStorage.setItem('mmg_token', data.token)
    localStorage.setItem('mmg_user', data.username)
    localStorage.setItem('mmg_role', data.role)
  }

  function logout() {
    token.value = ''
    username.value = ''
    role.value = ''
    localStorage.removeItem('mmg_token')
    localStorage.removeItem('mmg_user')
    localStorage.removeItem('mmg_role')
  }

  return { token, username, role, isLoggedIn, canWrite, login, logout }
})
