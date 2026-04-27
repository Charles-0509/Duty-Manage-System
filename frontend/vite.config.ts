import path from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    AutoImport({
      resolvers: [ElementPlusResolver({ importStyle: 'css' })],
    }),
    Components({
      resolvers: [ElementPlusResolver({ importStyle: 'css' })],
    }),
  ],
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

          if (id.includes('@element-plus/icons-vue')) {
            return 'vendor-element-icons'
          }

          if (id.includes('element-plus') || id.includes('@element-plus')) {
            if (id.includes('components/table')) {
              return 'vendor-element-table'
            }
            if (id.includes('components/date-picker') || id.includes('components/time-picker')) {
              return 'vendor-element-date'
            }
            if (
              id.includes('components/select') ||
              id.includes('components/option') ||
              id.includes('components/input') ||
              id.includes('components/input-number') ||
              id.includes('components/checkbox') ||
              id.includes('components/form')
            ) {
              return 'vendor-element-form'
            }
            if (
              id.includes('components/dialog') ||
              id.includes('components/drawer') ||
              id.includes('components/message') ||
              id.includes('components/message-box') ||
              id.includes('components/loading') ||
              id.includes('components/overlay')
            ) {
              return 'vendor-element-overlay'
            }
            return 'vendor-element-core'
          }

          if (id.includes('@floating-ui') || id.includes('@popperjs')) {
            return 'vendor-floating'
          }

          if (id.includes('async-validator')) {
            return 'vendor-validator'
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
