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

async function submit() {
  if (!form.username || !form.password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  loading.value = true
  try {
    await authStore.loginWithPassword(form)
    ElMessage.success('登录成功')
    router.push('/dashboard')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <section class="login-hero">
      <div class="hero-copy">
        <span class="pill">Vue + Golang + SQLite</span>
        <h1>把值班排班、实际出勤与工时统计放进同一张控制台。</h1>
        <p>
          新版前后端分离实现，保留原系统的角色与业务流，同时把登录、排班、实际值班和工单统计整理成更顺手的日常工具。
        </p>
      </div>
      <div class="hero-metrics">
        <div class="hero-card glass-card">
          <p class="section-label">默认账号</p>
          <strong>管理员：admin / admin</strong>
          <p class="muted">普通用户默认用户名与密码均为姓名拼音，首次登录会被要求改密。</p>
        </div>
        <div class="hero-card glass-card">
          <p class="section-label">适用角色</p>
          <strong>值班人员 / 管理员 / 人事专员</strong>
          <p class="muted">覆盖空闲时间登记、计划排班、实际值班调整、工单与用户管理。</p>
        </div>
      </div>
    </section>

    <section class="login-card glass-card">
      <div>
        <p class="section-label">Sign In</p>
        <h2>欢迎回来</h2>
        <p class="muted">登录后即可进入值班工作台。</p>
      </div>

      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="例如：admin 或 yezifeng" size="large" />
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
  grid-template-columns: minmax(0, 1.15fr) minmax(360px, 420px);
  gap: 28px;
  padding: 28px;
}

.login-hero,
.login-card {
  padding: 34px;
}

.login-hero {
  display: grid;
  gap: 24px;
  align-content: space-between;
}

.hero-copy h1 {
  margin: 18px 0 18px;
  font-size: clamp(2.4rem, 5vw, 4.6rem);
  line-height: 0.95;
  letter-spacing: -0.05em;
}

.hero-copy p {
  max-width: 760px;
  font-size: 1.05rem;
  line-height: 1.75;
  color: var(--muted);
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
}

.login-card h2 {
  margin: 10px 0 8px;
  font-size: 2rem;
}

.login-btn {
  width: 100%;
  margin-top: 6px;
}

@media (max-width: 980px) {
  .login-page {
    grid-template-columns: 1fr;
    padding: 16px;
  }

  .hero-metrics {
    grid-template-columns: 1fr;
  }
}
</style>
