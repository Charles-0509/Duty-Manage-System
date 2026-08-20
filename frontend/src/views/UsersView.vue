<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createUser,
  fetchUsers,
  removeUserMembership,
  resetUserPassword,
  restoreUserMembership,
  updateUserProfile,
  updateUserStatus,
} from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import type { CreateMemberPayload, Role, User } from '@/types'

const metaStore = useMetaStore()
const loading = ref(false)
const drawerVisible = ref(false)
const createVisible = ref(false)
const users = ref<User[]>([])
const selectedUser = ref<User | null>(null)
const profileDraft = reactive({ realName: '', studentNumber: '', role: 'USER' as Role, sortOrder: 0 })
const passwordDraft = reactive({ value: '', loading: false })
const createForm = reactive<CreateMemberPayload>({
  username: '',
  realName: '',
  studentNumber: '',
  role: 'USER',
  initialPassword: '',
})

const archived = computed(() => Boolean(metaStore.config?.semester.archived))
onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadData()
})

function displayRole(role: Role) {
  return metaStore.config?.userRoles[role] || role
}

async function loadData() {
  loading.value = true
  try {
    users.value = await fetchUsers()
  } catch {
    ElMessage.error('加载成员数据失败')
  } finally {
    loading.value = false
  }
}

function openDrawer(user: User) {
  selectedUser.value = user
  profileDraft.realName = user.realName
  profileDraft.studentNumber = user.studentNumber
  profileDraft.role = user.role
  profileDraft.sortOrder = user.sortOrder
  passwordDraft.value = ''
  drawerVisible.value = true
}

function openCreate() {
  Object.assign(createForm, { username: '', realName: '', studentNumber: '', role: 'USER', initialPassword: '' })
  createVisible.value = true
}

async function submitCreate() {
  try {
    await createUser({
      username: createForm.username.trim(),
      realName: createForm.realName.trim(),
      studentNumber: createForm.studentNumber.trim(),
      role: createForm.role,
      initialPassword: createForm.initialPassword,
    })
    createVisible.value = false
    await Promise.all([loadData(), metaStore.reload()])
    ElMessage.success('成员已加入当前学期')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '新增成员失败')
  }
}

async function saveProfile() {
  if (!selectedUser.value) return
  try {
    await updateUserProfile(selectedUser.value.id, {
      realName: profileDraft.realName.trim(),
      studentNumber: profileDraft.studentNumber.trim(),
      role: profileDraft.role,
      sortOrder: profileDraft.sortOrder,
    })
    await Promise.all([loadData(), metaStore.reload()])
    const updated = users.value.find((item) => item.id === selectedUser.value?.id)
    if (updated) openDrawer(updated)
    ElMessage.success('成员资料已更新')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '更新成员资料失败')
  }
}

async function toggleStatus() {
  if (!selectedUser.value) return
  await updateUserStatus(selectedUser.value.id, !selectedUser.value.isActive)
  await loadData()
  selectedUser.value = users.value.find((item) => item.id === selectedUser.value?.id) || null
}

async function submitPasswordReset() {
  if (!selectedUser.value || !passwordDraft.value) {
    ElMessage.warning('请输入新密码')
    return
  }
  passwordDraft.loading = true
  try {
    await resetUserPassword(selectedUser.value.id, passwordDraft.value)
    passwordDraft.value = ''
    ElMessage.success('密码已重置，登录限制已清除')
  } finally {
    passwordDraft.loading = false
  }
}

async function removeMembership() {
  if (!selectedUser.value) return
  await ElMessageBox.confirm(`将“${selectedUser.value.realName}”移出当前学期？历史记录会继续保留。`, '移出学期', { type: 'warning' })
  await removeUserMembership(selectedUser.value.id)
  drawerVisible.value = false
  await Promise.all([loadData(), metaStore.reload()])
}

async function restoreMembership() {
  if (!selectedUser.value) return
  await restoreUserMembership(selectedUser.value.id)
  await Promise.all([loadData(), metaStore.reload()])
  selectedUser.value = users.value.find((item) => item.id === selectedUser.value?.id) || null
  ElMessage.success('成员已恢复到当前学期')
}

</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Members</p>
        <h2 class="page-title">用户管理</h2>
        <p class="page-subtitle">{{ metaStore.config?.semester.name }}</p>
      </div>
      <el-button type="primary" :disabled="archived" @click="openCreate">新增成员</el-button>
    </section>

    <section class="table-band">
      <div class="responsive-table" style="--table-min-width: 940px">
      <el-table :data="users" empty-text="暂无成员">
        <el-table-column prop="realName" label="姓名" min-width="150" />
        <el-table-column prop="studentNumber" label="学号" min-width="160">
          <template #default="{ row }">{{ row.studentNumber || '未维护' }}</template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" min-width="160" />
        <el-table-column label="角色" width="130">
          <template #default="{ row }"><el-tag>{{ displayRole(row.role as Role) }}</el-tag></template>
        </el-table-column>
        <el-table-column label="账户" width="100">
          <template #default="{ row }"><el-tag :type="row.isActive ? 'success' : 'danger'">{{ row.isActive ? '启用' : '停用' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="学期成员" width="110">
          <template #default="{ row }"><el-tag :type="row.semesterMember ? 'success' : 'info'">{{ row.semesterMember ? '在册' : '已移出' }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="createdAt" label="创建时间" min-width="170" />
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }"><el-button type="primary" text @click="openDrawer(row)">管理</el-button></template>
        </el-table-column>
      </el-table>
      </div>
    </section>

    <el-drawer v-model="drawerVisible" :title="selectedUser?.realName || '成员管理'" size="440px">
      <template v-if="selectedUser">
        <div v-if="selectedUser.role !== 'ADMIN'" class="drawer-section">
          <p class="section-label">学期成员</p>
          <el-form label-position="top">
            <el-form-item label="姓名"><el-input v-model="profileDraft.realName" :disabled="archived" /></el-form-item>
            <el-form-item label="学号"><el-input v-model="profileDraft.studentNumber" :disabled="archived" inputmode="numeric" maxlength="32" /></el-form-item>
            <el-form-item label="角色">
              <el-select v-model="profileDraft.role" :disabled="archived" style="width: 100%">
                <el-option label="值班人员" value="USER" />
                <el-option label="组长" value="LEADER" />
                <el-option label="负责人" value="OWNER" />
                <el-option label="人事专员" value="HR" />
                <el-option label="财务" value="FINANCE" />
              </el-select>
            </el-form-item>
            <el-form-item label="成员排序">
              <el-input-number v-model="profileDraft.sortOrder" :min="1" :disabled="archived" style="width: 100%" />
            </el-form-item>
          </el-form>
          <el-button type="primary" :disabled="archived || !selectedUser.semesterMember" @click="saveProfile">保存成员资料</el-button>
          <el-button v-if="selectedUser.semesterMember" type="danger" plain :disabled="archived" @click="removeMembership">移出本学期</el-button>
          <el-button v-else type="success" plain :disabled="archived" @click="restoreMembership">恢复到本学期</el-button>
        </div>

        <div class="drawer-section">
          <p class="section-label">全局账户</p>
          <el-input v-model="passwordDraft.value" show-password placeholder="输入新密码" />
          <el-button type="primary" class="section-action" :loading="passwordDraft.loading" @click="submitPasswordReset">重置密码</el-button>
          <el-button :type="selectedUser.isActive ? 'danger' : 'success'" plain class="section-action" @click="toggleStatus">
            {{ selectedUser.isActive ? '停用账户' : '启用账户' }}
          </el-button>
        </div>
      </template>
    </el-drawer>

    <el-dialog v-model="createVisible" title="新增学期成员" width="500px">
      <el-form label-position="top">
        <el-form-item label="用户名"><el-input v-model="createForm.username" /></el-form-item>
        <el-form-item label="姓名"><el-input v-model="createForm.realName" /></el-form-item>
        <el-form-item label="学号"><el-input v-model="createForm.studentNumber" inputmode="numeric" maxlength="32" /></el-form-item>
        <el-form-item label="学期角色">
          <el-select v-model="createForm.role" style="width: 100%">
            <el-option label="值班人员" value="USER" />
            <el-option label="组长" value="LEADER" />
            <el-option label="负责人" value="OWNER" />
            <el-option label="人事专员" value="HR" />
            <el-option label="财务" value="FINANCE" />
          </el-select>
        </el-form-item>
        <el-form-item label="初始密码"><el-input v-model="createForm.initialPassword" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" @click="submitCreate">添加</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.table-band {
  min-width: 0;
  max-width: 100%;
  border-top: 1px solid rgba(24, 48, 66, 0.1);
}

.drawer-section {
  margin-bottom: 28px;
  padding-bottom: 24px;
  border-bottom: 1px solid rgba(24, 48, 66, 0.1);
}

.section-action {
  margin-top: 12px;
}
</style>
