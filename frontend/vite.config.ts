import path from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined
          }

          if (id.includes('echarts') || id.includes('zrender') || id.includes('vue-echarts')) {
            return 'vendor-charts'
          }

          if (
            id.includes('element-plus') ||
            id.includes('@element-plus') ||
            id.includes('@floating-ui') ||
            id.includes('@popperjs') ||
            id.includes('async-validator')
          ) {
            return 'vendor-element-plus'
          }

          if (id.includes('vue-router')) {
            return 'vendor-router'
          }

          if (id.includes('pinia')) {
            return 'vendor-pinia'
          }

          if (id.includes('axios') || id.includes('dayjs')) {
            return 'vendor-utils'
          }

          if (
            id.includes('/vue/') ||
            id.includes('\\vue\\') ||
            id.includes('@vue') ||
            id.includes('vue-demi')
          ) {
            return 'vendor-vue'
          }

          return 'vendor-misc'
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
})
