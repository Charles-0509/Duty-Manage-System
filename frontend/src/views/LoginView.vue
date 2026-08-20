<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
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
      <div class="hero-copy">
        <span class="pill">Duty Manage System</span>
        <h1>机房管理系统</h1>
      </div>
      <div class="hero-metrics">
        <div class="hero-card glass-card">
          <p class="section-label">账号安全</p>
          <strong>请使用管理员分配的用户名和初始密码</strong>
          <p class="muted">首次登录后必须先设置新的安全密码。</p>
        </div>
        <div class="hero-card glass-card">
          <p class="section-label">适用角色</p>
          <strong>值班成员 / 组长 / 负责人 / 管理员 / 人事专员</strong>
          <p class="muted">覆盖机房值班安排所需的主要流程，方便内部协作与日常维护。</p>
        </div>
      </div>
    </section>

    <section class="login-card glass-card">
      <div>
        <p class="section-label">Sign In</p>
        <h2>欢迎登录</h2>
        <p class="muted">登录后进入机房管理系统。</p>
      </div>

      <el-alert
        v-if="loginError"
        :title="loginError"
        :type="loginErrorType"
        show-icon
        :closable="false"
        class="login-alert"
      />

      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="姓名全拼小写" size="large" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" show-password placeholder="请输入密码" size="large" @keyup.enter="submit" />
        </el-form-item>
        <el-button type="primary" size="large" class="login-btn" :loading="loading" @click="submit">
          登录系统
        </el-button>
      </el-form>
    </section>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(360px, 1fr) minmax(360px, 460px);
  align-items: center;
  gap: clamp(24px, 5vw, 88px);
  width: min(100%, 1760px);
  margin: 0 auto;
  padding: clamp(20px, 4vw, 72px);
}

.login-hero,
.login-card {
  padding: clamp(24px, 3vw, 44px);
}

.login-hero {
  display: grid;
  gap: clamp(24px, 6vh, 72px);
  align-content: center;
  min-height: min(720px, calc(100vh - 144px));
}

.hero-copy h1 {
  margin: 18px 0 18px;
  font-size: clamp(3rem, 5vw, 5.4rem);
  line-height: 1.02;
  letter-spacing: 0;
}

.hero-metrics {
  display: grid;
  gap: 18px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.hero-card {
  padding: 22px;
}

.hero-card strong {
  display: block;
  margin-bottom: 8px;
  font-size: 1.1rem;
}

.login-card {
  align-self: center;
  width: min(100%, 460px);
  justify-self: center;
}

.login-card h2 {
  margin: 10px 0 8px;
  font-size: 2rem;
}

.login-btn {
  width: 100%;
  margin-top: 6px;
}

.login-alert {
  margin: 20px 0 4px;
}

@media (max-width: 980px) {
  .login-page {
    grid-template-columns: 1fr;
    align-items: start;
    gap: 18px;
    padding: 18px;
  }

  .login-hero {
    min-height: auto;
    gap: 18px;
  }

  .hero-metrics {
    grid-template-columns: 1fr;
  }

  .login-card {
    width: 100%;
  }
}

@media (max-width: 640px) {
  .login-page {
    padding: 12px;
  }

  .login-hero,
  .login-card {
    padding: 20px;
  }

  .hero-copy h1 {
    margin: 14px 0 4px;
    font-size: clamp(2.2rem, 14vw, 3.4rem);
  }

  .hero-card {
    padding: 18px;
  }

  .login-card h2 {
    font-size: 1.7rem;
  }
}
</style>
