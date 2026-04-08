<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchUsers, resetUserPassword, updateUserRole, updateUserStatus } from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import type { Role, User } from '@/types'

const metaStore = useMetaStore()
const loading = ref(false)
const drawerVisible = ref(false)
const users = ref<User[]>([])
const selectedUser = ref<User | null>(null)
const roleDraft = ref<'USER' | 'HR'>('USER')
const passwordDraft = reactive({
  value: '',
  loading: false,
})

const roleLabel = computed<Record<Role, string>>(
  () =>
    metaStore.config?.userRoles || {
      USER: '值班人员',
      ADMIN: '管理员',
      HR: '人事专员',
    },
)

function displayRole(role: Role) {
  return roleLabel.value[role] || role
}

onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadUsers()
})

async function loadUsers() {
  loading.value = true
  try {
    users.value = await fetchUsers()
  } catch {
    ElMessage.error('加载用户失败')
  } finally {
    loading.value = false
  }
}

function openDrawer(user: User) {
  selectedUser.value = user
  roleDraft.value = user.role === 'HR' ? 'HR' : 'USER'
  passwordDraft.value = ''
  drawerVisible.value = true
}

async function saveRole() {
  if (!selectedUser.value) return
  await updateUserRole(selectedUser.value.id, roleDraft.value)
  ElMessage.success('角色更新成功')
  await loadUsers()
}

async function toggleStatus() {
  if (!selectedUser.value) return
  await updateUserStatus(selectedUser.value.id, !selectedUser.value.isActive)
  ElMessage.success('用户状态已更新')
  await loadUsers()
  selectedUser.value = users.value.find((item: User) => item.id === selectedUser.value?.id) || null
}

async function submitPasswordReset() {
  if (!selectedUser.value || !passwordDraft.value) {
    ElMessage.warning('请输入新密码')
    return
  }

  passwordDraft.loading = true
  try {
    await resetUserPassword(selectedUser.value.id, passwordDraft.value)
    ElMessage.success('密码已重置')
    passwordDraft.value = ''
  } finally {
    passwordDraft.loading = false
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Users</p>
        <h2 class="page-title">用户管理</h2>
        <p class="page-subtitle">
          管理员可以查看账户、调整角色、重置密码，并启用或停用用户。
        </p>
      </div>
    </section>

    <section class="glass-card">
      <el-table :data="users" empty-text="暂无用户">
        <el-table-column prop="realName" label="姓名" min-width="160" />
        <el-table-column prop="username" label="用户名" min-width="180" />
        <el-table-column label="角色" width="140">
          <template #default="{ row }">
            <el-tag>{{ displayRole(row.role as Role) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="140">
          <template #default="{ row }">
            <el-tag :type="row.isActive ? 'success' : 'danger'">
              {{ row.isActive ? '激活' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="createdAt" label="创建时间" min-width="180" />
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" text @click="openDrawer(row)">管理</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-drawer v-model="drawerVisible" :title="selectedUser?.realName || '用户管理'" size="420px">
      <template v-if="selectedUser">
        <div class="drawer-section">
          <p class="section-label">账户信息</p>
          <div class="info-grid">
            <div>
              <span class="muted">用户名</span>
              <strong>{{ selectedUser.username }}</strong>
            </div>
            <div>
              <span class="muted">角色</span>
              <strong>{{ displayRole(selectedUser.role) }}</strong>
            </div>
          </div>
        </div>

        <div class="drawer-section" v-if="selectedUser.role !== 'ADMIN'">
          <p class="section-label">角色设置</p>
          <el-select v-model="roleDraft" style="width: 100%">
            <el-option label="值班人员" value="USER" />
            <el-option label="人事专员" value="HR" />
          </el-select>
          <el-button type="primary" style="margin-top: 12px" @click="saveRole">更新角色</el-button>
        </div>

        <div class="drawer-section">
          <p class="section-label">密码重置</p>
          <el-input v-model="passwordDraft.value" show-password placeholder="输入新密码" />
          <el-button type="primary" style="margin-top: 12px" :loading="passwordDraft.loading" @click="submitPasswordReset">
            重置密码并强制下次改密
          </el-button>
        </div>

        <div class="drawer-section">
          <p class="section-label">账户状态</p>
          <el-button :type="selectedUser.isActive ? 'danger' : 'success'" plain @click="toggleStatus">
            {{ selectedUser.isActive ? '停用用户' : '激活用户' }}
          </el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped>
.glass-card {
  padding: 24px;
}

.drawer-section {
  margin-bottom: 26px;
}

.info-grid {
  display: grid;
  gap: 14px;
}

.info-grid span,
.info-grid strong {
  display: block;
}

.info-grid strong {
  margin-top: 6px;
}
</style>
