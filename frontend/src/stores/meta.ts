import { ref } from 'vue'
import { defineStore } from 'pinia'
import { fetchMetaConfig } from '@/api/services'
import type { MetaConfig } from '@/types'

export const useMetaStore = defineStore('meta', () => {
  const config = ref<MetaConfig | null>(null)
  const loading = ref(false)

  async function ensureLoaded() {
    if (config.value || loading.value) return
    loading.value = true
    try {
      config.value = await fetchMetaConfig()
    } finally {
      loading.value = false
    }
  }

  return {
    config,
    loading,
    ensureLoaded,
  }
})
