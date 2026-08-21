<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Lock, Management, UserFilled } from '@element-plus/icons-vue'
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
  <div class="login-page">
    <section class="login-hero">
      <div class="hero-brand">
        <div class="hero-logo">
          <el-icon :size="28"><Management /></el-icon>
        </div>
        <div>
          <span class="hero-tag">DMS · Duty Manage System</span>
          <h1 class="hero-title">机房综合值班管理系统</h1>
        </div>
      </div>
      <p class="hero-desc">
        面向实验室与机房值班团队的一体化调度平台，覆盖时间意向收集、智能计划排班、实际值班调整与劳务工时结算。
      </p>

      <div class="hero-cards">
        <div class="info-card panel-card">
          <div class="info-card-icon security">
            <el-icon :size="18"><Lock /></el-icon>
          </div>
          <div>
            <strong>账号安全机制</strong>
            <p class="muted">初次分配账号登录后须重置密码，保障系统与数据权限安全。</p>
          </div>
        </div>

        <div class="info-card panel-card">
          <div class="info-card-icon roles">
            <el-icon :size="18"><UserFilled /></el-icon>
          </div>
          <div>
            <strong>角色协同工作流</strong>
            <p class="muted">支持普通成员、组长、负责人、财务与管理员的多维权限流转。</p>
          </div>
        </div>
      </div>
    </section>

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
            placeholder="姓名拼音或管理员账号"
            size="large"
            clearable
          />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="form.password"
            show-password
            placeholder="请输入密码"
            size="large"
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
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(340px, 1.2fr) minmax(360px, 440px);
  align-items: center;
  justify-content: center;
  gap: clamp(32px, 6vw, 96px);
  max-width: 1200px;
  margin: 0 auto;
  padding: 24px;
}

.login-hero {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.hero-brand {
  display: flex;
  align-items: center;
  gap: 16px;
}

.hero-logo {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 52px;
  height: 52px;
  border-radius: var(--radius-lg);
  background: linear-gradient(135deg, #0d9488, #0f766e);
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(13, 148, 136, 0.3);
}

.hero-tag {
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--primary);
  letter-spacing: 0.08em;
}

.hero-title {
  margin: 4px 0 0;
  font-size: clamp(1.8rem, 3.2vw, 2.4rem);
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.03em;
  line-height: 1.15;
}

.hero-desc {
  margin: 0;
  color: #475569;
  font-size: 1rem;
  line-height: 1.7;
}

.hero-cards {
  display: grid;
  gap: 14px;
  margin-top: 8px;
}

.info-card {
  display: flex;
  gap: 14px;
  padding: 16px 18px;
  background: #ffffff;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
}

.info-card-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: var(--radius-md);
  flex-shrink: 0;
}

.info-card-icon.security {
  background: #f0fdfa;
  color: #0d9488;
}

.info-card-icon.roles {
  background: #eff6ff;
  color: #3b82f6;
}

.info-card strong {
  display: block;
  font-size: 0.92rem;
  color: #0f172a;
  margin-bottom: 2px;
}

.info-card .muted {
  font-size: 0.82rem;
  line-height: 1.5;
  margin: 0;
}

.login-card {
  padding: 36px 32px;
  background: #ffffff;
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
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

.login-btn {
  margin-top: 10px;
  width: 100%;
  font-weight: 600;
  height: 44px;
  border-radius: var(--radius-md);
  background: var(--primary);
  border-color: var(--primary);
}

.login-btn:hover {
  background: var(--primary-hover);
  border-color: var(--primary-hover);
}

@media (max-width: 900px) {
  .login-page {
    grid-template-columns: 1fr;
    max-width: 480px;
    padding: 20px 16px;
    gap: 24px;
  }

  .login-hero {
    text-align: center;
    align-items: center;
  }

  .hero-brand {
    flex-direction: column;
    text-align: center;
  }

  .hero-cards {
    display: none;
  }
}
</style>
