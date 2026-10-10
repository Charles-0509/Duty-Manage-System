<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import AppLogo from '@/components/AppLogo.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const form = reactive({
  username: '',
  password: '',
})
const loading = ref(false)
const loginError = ref('')
const loginErrorType = ref<'error' | 'warning'>('error')

async function submit() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  loginError.value = ''
  loading.value = true
  try {
    await authStore.loginWithPassword(form)
    ElMessage.success('登录成功')
    router.push('/dashboard')
  } catch (error: any) {
    loginError.value = error?.response?.data?.message || '登录失败'
    loginErrorType.value = error?.response?.status === 429 ? 'warning' : 'error'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrapper">
    <div class="login-container">
      <!-- 品牌区在竖屏时显示于登录框上方 -->
      <section class="login-hero">
        <div class="hero-brand">
          <AppLogo :size="56" />
          <div class="brand-text">
            <span class="hero-tag">Duty Manage System</span>
            <h1 class="hero-title">机房管理系统</h1>
          </div>
        </div>
      </section>

      <!-- 登录卡片 -->
      <section class="login-card panel-card">
        <div class="login-header">
          <h2 class="login-heading">用户登录</h2>
          <p class="login-subtext">请输入您的账号密码以访问系统</p>
        </div>

        <el-alert
          v-if="loginError"
          :title="loginError"
          :type="loginErrorType"
          show-icon
          :closable="false"
          class="login-alert"
        />

        <el-form label-position="top" class="login-form" @submit.prevent="submit">
          <el-form-item label="用户名">
            <el-input
              v-model="form.username"
              placeholder="姓名全拼或管理员账号"
              size="large"
              autocomplete="username"
              autocapitalize="none"
              :spellcheck="false"
              clearable
            />
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="form.password"
              show-password
              placeholder="请输入密码"
              size="large"
              autocomplete="current-password"
              @keyup.enter="submit"
            />
          </el-form-item>
          <el-button
            type="primary"
            size="large"
            class="login-btn"
            :loading="loading"
            @click="submit"
          >
            登 录
          </el-button>
        </el-form>
      </section>
    </div>
  </div>
</template>

<style scoped>
.login-wrapper {
  min-height: 100vh;
  min-height: 100svh;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 24px;
  padding: max(24px, env(safe-area-inset-top)) max(24px, env(safe-area-inset-right))
    max(24px, env(safe-area-inset-bottom)) max(24px, env(safe-area-inset-left));
}

.login-container {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  max-width: 900px;
  gap: clamp(36px, 6vw, 72px);
  margin: 0 auto;
}

.login-hero {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 0;
}

.hero-brand {
  display: flex;
  align-items: center;
  gap: 20px;
}

.brand-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 6px;
}

.hero-tag {
  font-size: 0.86rem;
  font-weight: 700;
  color: #2563eb;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.hero-title {
  margin: 0;
  font-size: clamp(2rem, 3vw, 2.5rem);
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.02em;
  line-height: 1.15;
  white-space: nowrap;
}

.login-card {
  width: min(380px, 100%);
  min-width: 0;
  flex-shrink: 0;
  padding: 36px 30px;
  background: #ffffff;
  border-radius: var(--radius-xl);
  box-shadow: 0 10px 25px -5px rgba(15, 23, 42, 0.06), 0 8px 10px -6px rgba(15, 23, 42, 0.04);
  border: 1px solid var(--line);
}

.login-header {
  margin-bottom: 24px;
}

.login-heading {
  margin: 0;
  font-size: 1.5rem;
  font-weight: 700;
  color: #0f172a;
}

.login-subtext {
  margin: 6px 0 0;
  color: var(--muted);
  font-size: 0.88rem;
}

.login-alert {
  margin-bottom: 18px;
}

.login-form {
  display: flex;
  flex-direction: column;
}

.login-form :deep(.el-input__wrapper) {
  min-height: 48px;
}

.login-form :deep(.el-input__inner) {
  font-size: max(16px, 1rem);
}

.login-btn {
  margin-top: 10px;
  width: 100%;
  font-weight: 600;
  font-size: 1rem;
  min-height: 48px;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, #0d9488, #0f766e);
  border: none;
  box-shadow: 0 4px 12px rgba(13, 148, 136, 0.25);
  transition: all 0.15s ease;
}

.login-btn:hover {
  background: linear-gradient(135deg, #0f766e, #115e59);
  box-shadow: 0 6px 16px rgba(13, 148, 136, 0.35);
  transform: translateY(-1px);
}

/* 竖屏按窗口比例上下排列；窄横屏也保留足够的表单宽度。 */
@media (max-aspect-ratio: 1/1), (max-width: 760px) {
  .login-wrapper {
    padding: max(24px, env(safe-area-inset-top)) max(16px, env(safe-area-inset-right))
      max(24px, env(safe-area-inset-bottom)) max(16px, env(safe-area-inset-left));
  }

  .login-container {
    flex-direction: column;
    align-items: center;
    max-width: 420px;
    gap: clamp(24px, 4svh, 40px);
  }

  .login-hero {
    flex: none;
    width: 100%;
    align-items: center;
    text-align: center;
  }

  .hero-brand {
    justify-content: center;
    gap: clamp(12px, 3vw, 20px);
  }

  .brand-text {
    align-items: center;
  }

  .hero-title {
    font-size: clamp(1.6rem, 6vw, 2rem);
    white-space: normal;
  }

  .login-card {
    width: 100%;
    padding: clamp(24px, 5vw, 32px) clamp(20px, 5vw, 28px);
    border-radius: var(--radius-lg);
  }
}
</style>
