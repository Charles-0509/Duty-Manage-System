<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  activateSemester,
  createUser,
  createSemester,
  deleteSemester,
  exportSemester,
  fetchSemesters,
  fetchSystemSettings,
  fetchUsers,
  importSemester,
  removeUserMembership,
  renameSemester,
  restoreUserMembership,
  setSemesterArchived,
  updateUserProfile,
  updateSystemSettings,
} from '@/api/services'
import { downloadBlob } from '@/utils/schedule'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import type {
  CreateMemberPayload,
  CreateSemesterPayload,
  Role,
  SemesterSummary,
  SystemSettings,
  UpdateSystemSettingsPayload,
  User,
} from '@/types'

const loading = ref(false)
const authStore = useAuthStore()
const metaStore = useMetaStore()
const saving = ref(false)
const switchingId = ref('')
const exportingId = ref('')
const currentSettings = ref<SystemSettings | null>(null)
const semesters = ref<SemesterSummary[]>([])
const createVisible = ref(false)
const importInput = ref<HTMLInputElement>()
const users = ref<User[]>([])
const memberView = ref<'active' | 'removed' | 'all'>('active')
const memberCreateVisible = ref(false)
const memberEditVisible = ref(false)
const selectedMember = ref<User | null>(null)

const form = reactive<UpdateSystemSettingsPayload>({
  firstMonday: '',
  laborSeed: '',
  workStudyContent: '',
  dutyRate: 25,
  workOrderRate: 50,
  mgmtLeaderRate: 800,
  mgmtOwnerRate: 1200,
})

const createForm = reactive<CreateSemesterPayload>({
  name: '',
  firstMonday: '',
  cloneFromId: '',
})

const memberCreateForm = reactive<CreateMemberPayload>({
  username: '',
  realName: '',
  role: 'USER',
  initialPassword: '',
})

const memberEditForm = reactive({
  realName: '',
  role: 'USER' as Role,
  sortOrder: 1,
})

const activeSemester = computed(() => semesters.value.find((item) => item.active) || currentSettings.value?.semester)
const settingsLocked = computed(() => Boolean(activeSemester.value?.archived))
const canManageSemesters = computed(() => authStore.hasRole(['ADMIN']))
const semesterMembers = computed(() => users.value.filter((user) => user.role !== 'ADMIN'))
const visibleMembers = computed(() => {
  if (memberView.value === 'active') return semesterMembers.value.filter((user) => user.semesterMember)
  if (memberView.value === 'removed') return semesterMembers.value.filter((user) => !user.semesterMember)
  return semesterMembers.value
})
const activeMemberCount = computed(() => semesterMembers.value.filter((user) => user.semesterMember).length)
const removedMemberCount = computed(() => semesterMembers.value.length - activeMemberCount.value)
const roleLabels: Record<Role, string> = {
  USER: '值班人员',
  LEADER: '组长',
  OWNER: '负责人',
  ADMIN: '管理员',
  HR: '人事专员',
  FINANCE: '财务',
}

onMounted(loadAll)

async function loadAll() {
  loading.value = true
  try {
    const settings = await fetchSystemSettings()
    const [semesterData, userItems] = await Promise.all([
      canManageSemesters.value ? fetchSemesters() : Promise.resolve({ items: [settings.semester], active: settings.semester }),
      canManageSemesters.value ? fetchUsers() : Promise.resolve([]),
    ])
    currentSettings.value = settings
    semesters.value = semesterData.items
    users.value = userItems
    form.firstMonday = settings.firstMonday
    form.laborSeed = settings.laborSeed || ''
    form.workStudyContent = settings.workStudyContent
    form.dutyRate = settings.dutyRate
    form.workOrderRate = settings.workOrderRate
    form.mgmtLeaderRate = settings.mgmtLeaderRate
    form.mgmtOwnerRate = settings.mgmtOwnerRate
  } catch {
    ElMessage.error('加载学期配置失败')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await updateSystemSettings({
      firstMonday: form.firstMonday.trim(),
      laborSeed: form.laborSeed.trim(),
      workStudyContent: form.workStudyContent.trim(),
      dutyRate: form.dutyRate,
      workOrderRate: form.workOrderRate,
      mgmtLeaderRate: form.mgmtLeaderRate,
      mgmtOwnerRate: form.mgmtOwnerRate,
    })
    await loadAll()
    ElMessage.success('学期配置已立即生效')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '保存学期配置失败')
  } finally {
    saving.value = false
  }
}

function openCreate() {
  createForm.name = ''
  createForm.firstMonday = ''
  createForm.cloneFromId = activeSemester.value?.id || ''
  createVisible.value = true
}

async function submitCreate() {
  try {
    await createSemester({
      name: createForm.name.trim(),
      firstMonday: createForm.firstMonday.trim(),
      cloneFromId: createForm.cloneFromId,
    })
    createVisible.value = false
    await loadAll()
    ElMessage.success('草稿学期已创建，可检查成员后再启用')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '创建学期失败')
  }
}

async function activate(item: SemesterSummary) {
  await ElMessageBox.confirm(
    item.archived
      ? `切换到归档学期“${item.name}”后，全系统将进入该学期的只读视图。`
      : `启用“${item.name}”后，全系统立即切换；当前未归档学期会自动归档。`,
    '确认切换学期',
    { type: 'warning', confirmButtonText: '确认切换' },
  )
  switchingId.value = item.id
  try {
    await activateSemester(item.id)
    ElMessage.success('学期已切换，页面正在刷新')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '切换学期失败')
  } finally {
    switchingId.value = ''
  }
}

async function toggleArchive(item: SemesterSummary) {
  try {
    await setSemesterArchived(item.id, !item.archived)
    await loadAll()
    ElMessage.success(item.archived ? '已解除归档' : '学期已归档')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '更新归档状态失败')
  }
}

async function rename(item: SemesterSummary) {
  const result = await ElMessageBox.prompt('输入新的学期名称', '修改学期名称', {
    inputValue: item.name,
    inputValidator: (value) => Boolean(value.trim()) || '名称不能为空',
  })
  await renameSemester(item.id, result.value.trim())
  await loadAll()
}

async function removeDraft(item: SemesterSummary) {
  await ElMessageBox.confirm(`删除草稿“${item.name}”？此操作会删除对应的空学期数据库。`, '删除草稿', { type: 'warning' })
  await deleteSemester(item.id)
  await loadAll()
}

async function downloadDatabase(item: SemesterSummary) {
  exportingId.value = item.id
  try {
    const blob = await exportSemester(item.id)
    downloadBlob(blob, `${safeFilename(item.name)}.db`)
  } catch {
    ElMessage.error('导出学期数据库失败')
  } finally {
    exportingId.value = ''
  }
}

function chooseImport() {
  importInput.value?.click()
}

async function handleImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    await importSemester(file)
    await loadAll()
    ElMessage.success('学期数据库已导入并保持归档状态')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '导入学期数据库失败')
  }
}

function safeFilename(value: string) {
  return value.replace(/[\\/:*?"<>|]/g, '-').trim() || 'semester'
}

function displayRole(role: Role) {
  return roleLabels[role] || role
}

function openMemberCreate() {
  Object.assign(memberCreateForm, { username: '', realName: '', role: 'USER', initialPassword: '' })
  memberCreateVisible.value = true
}

async function submitMemberCreate() {
  try {
    await createUser({
      username: memberCreateForm.username.trim(),
      realName: memberCreateForm.realName.trim(),
      role: memberCreateForm.role,
      initialPassword: memberCreateForm.initialPassword,
    })
    memberCreateVisible.value = false
    await reloadMemberDirectory()
    memberView.value = 'active'
    ElMessage.success('成员已加入当前学期')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '新增成员失败')
  }
}

function openMemberEdit(member: User) {
  selectedMember.value = member
  memberEditForm.realName = member.realName
  memberEditForm.role = member.role
  memberEditForm.sortOrder = member.sortOrder
  memberEditVisible.value = true
}

async function saveMember() {
  if (!selectedMember.value) return
  try {
    await updateUserProfile(selectedMember.value.id, {
      realName: memberEditForm.realName.trim(),
      role: memberEditForm.role,
      sortOrder: memberEditForm.sortOrder,
    })
    memberEditVisible.value = false
    await reloadMemberDirectory()
    ElMessage.success('成员资料已更新')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '更新成员资料失败')
  }
}

async function removeMember(member: User) {
  await ElMessageBox.confirm(
    `将“${member.realName}”移出当前学期？已有排班、工单和财务历史不会删除。`,
    '移出本学期',
    { type: 'warning', confirmButtonText: '确认移出' },
  )
  try {
    await removeUserMembership(member.id)
    await reloadMemberDirectory()
    ElMessage.success('成员已移出当前学期')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '移出成员失败')
  }
}

async function restoreMember(member: User) {
  try {
    await restoreUserMembership(member.id)
    await reloadMemberDirectory()
    memberView.value = 'active'
    ElMessage.success('成员已恢复到当前学期')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '恢复成员失败')
  }
}

async function reloadMemberDirectory() {
  const [userItems] = await Promise.all([fetchUsers(), metaStore.reload()])
  users.value = userItems
}
</script>

<template>
  <div class="page-shell" data-page="system-settings" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Semester</p>
        <h2 class="page-title">学期管理</h2>
        <p class="page-subtitle">当前学期：{{ activeSemester?.name || '-' }}。切换对全系统立即生效。</p>
      </div>
      <div class="toolbar-actions">
        <template v-if="canManageSemesters">
          <input ref="importInput" class="hidden-input" type="file" accept=".db" @change="handleImport" />
          <el-button @click="chooseImport">导入数据库</el-button>
          <el-button type="primary" @click="openCreate">新建学期</el-button>
        </template>
      </div>
    </section>

    <section class="semester-list">
      <article v-for="item in semesters" :key="item.id" class="semester-row" :class="{ active: item.active }">
        <div class="semester-main">
          <div class="semester-title">
            <strong>{{ item.name }}</strong>
            <span v-if="item.active" class="state state-active">当前</span>
            <span v-if="item.draft" class="state">草稿</span>
            <span v-if="item.archived" class="state state-archived">已归档</span>
          </div>
          <p class="muted">单双周基准：{{ item.firstMonday }} · 创建于 {{ item.createdAt }}</p>
        </div>
        <div v-if="canManageSemesters" class="row-actions">
          <el-button v-if="!item.active" :loading="switchingId === item.id" @click="activate(item)">切换</el-button>
          <el-button @click="rename(item)">重命名</el-button>
          <el-button @click="toggleArchive(item)">{{ item.archived ? '解除归档' : '归档' }}</el-button>
          <el-button :loading="exportingId === item.id" @click="downloadDatabase(item)">导出</el-button>
          <el-button v-if="item.draft && !item.active" type="danger" plain @click="removeDraft(item)">删除</el-button>
        </div>
      </article>
    </section>

    <section v-if="canManageSemesters" class="members-band">
      <div class="card-header members-header">
        <div>
          <p class="section-label">Duty Members</p>
          <h3>当前学期值班成员</h3>
          <p class="muted">在册 {{ activeMemberCount }} 人，已移出 {{ removedMemberCount }} 人</p>
        </div>
        <el-button type="primary" :disabled="settingsLocked" @click="openMemberCreate">新增成员</el-button>
      </div>

      <div class="member-toolbar">
        <el-radio-group v-model="memberView" size="small">
          <el-radio-button value="active">当前在册</el-radio-button>
          <el-radio-button value="removed">已移出</el-radio-button>
          <el-radio-button value="all">全部</el-radio-button>
        </el-radio-group>
        <span v-if="settingsLocked" class="pill">归档学期只读</span>
      </div>

      <div class="responsive-table members-table-wrap" style="--table-min-width: 786px">
      <el-table :data="visibleMembers" empty-text="暂无符合条件的成员" class="members-table">
        <el-table-column prop="sortOrder" label="排序" width="76" />
        <el-table-column prop="realName" label="姓名" min-width="130" />
        <el-table-column prop="username" label="登录用户名" min-width="150" />
        <el-table-column label="学期角色" min-width="120">
          <template #default="{ row }">
            <el-tag>{{ displayRole(row.role as Role) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.semesterMember ? 'success' : 'info'">
              {{ row.semesterMember ? '在册' : '已移出' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="210" fixed="right">
          <template #default="{ row }">
            <template v-if="row.semesterMember">
              <el-button type="primary" text :disabled="settingsLocked" @click="openMemberEdit(row)">编辑</el-button>
              <el-button type="danger" text :disabled="settingsLocked" @click="removeMember(row)">移出本学期</el-button>
            </template>
            <el-button v-else type="success" text :disabled="settingsLocked" @click="restoreMember(row)">恢复到本学期</el-button>
          </template>
        </el-table-column>
      </el-table>
      </div>
    </section>

    <section class="settings-band">
      <div class="card-header">
        <div>
          <p class="section-label">Active Semester</p>
          <h3>当前学期配置</h3>
        </div>
        <span class="pill">{{ settingsLocked ? '只读归档' : '立即生效' }}</span>
      </div>
      <el-form label-position="top" class="settings-form">
        <el-form-item label="单双周起始 FIRST_MONDAY">
          <el-input v-model="form.firstMonday" maxlength="8" :disabled="settingsLocked" placeholder="20260907" />
        </el-form-item>
        <el-form-item label="默认劳务随机种子 SEED">
          <el-input v-model="form.laborSeed" :disabled="settingsLocked" placeholder="留空时使用随机结果" />
        </el-form-item>
        <el-form-item label="勤工助学记录表工作内容">
          <el-input v-model="form.workStudyContent" :disabled="settingsLocked" />
        </el-form-item>
        <el-form-item label="值班时薪（元/小时）">
          <el-input-number v-model="form.dutyRate" :min="0.01" :max="10000" :precision="2" :step="0.5" :disabled="settingsLocked" style="width: 100%" />
        </el-form-item>
        <el-form-item label="工单时薪（元/小时）">
          <el-input-number v-model="form.workOrderRate" :min="0.01" :max="10000" :precision="2" :step="0.5" :disabled="settingsLocked" style="width: 100%" />
        </el-form-item>
        <el-form-item label="组长/人事项目管理薪（元/月）">
          <el-input-number v-model="form.mgmtLeaderRate" :min="0" :max="10000" :precision="2" :step="50" :disabled="settingsLocked" style="width: 100%" />
        </el-form-item>
        <el-form-item label="负责人项目管理薪（元/月）">
          <el-input-number v-model="form.mgmtOwnerRate" :min="0" :max="10000" :precision="2" :step="50" :disabled="settingsLocked" style="width: 100%" />
        </el-form-item>
        <el-button type="primary" :loading="saving" :disabled="settingsLocked" @click="saveSettings">保存配置</el-button>
      </el-form>
      <p class="muted infrastructure-note">
        服务端口、JWT、备份仓库和全局模板目录继续由服务器环境配置维护；导出的学期数据库不包含全局 Word 模板。
      </p>
    </section>

    <el-dialog v-model="createVisible" title="新建学期" width="520px">
      <el-form label-position="top">
        <el-form-item label="学期名称">
          <el-input v-model="createForm.name" placeholder="2026-2027-1" />
        </el-form-item>
        <el-form-item label="单双周起始星期一">
          <el-input v-model="createForm.firstMonday" maxlength="8" placeholder="20260907" />
        </el-form-item>
        <el-form-item label="复制成员来源">
          <el-select v-model="createForm.cloneFromId" style="width: 100%">
            <el-option v-for="item in semesters" :key="item.id" :label="item.name" :value="item.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" @click="submitCreate">创建草稿</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="memberCreateVisible" title="新增当前学期成员" width="500px">
      <el-form label-position="top">
        <el-form-item label="登录用户名">
          <el-input v-model="memberCreateForm.username" autocomplete="off" />
        </el-form-item>
        <el-form-item label="姓名">
          <el-input v-model="memberCreateForm.realName" autocomplete="off" />
        </el-form-item>
        <el-form-item label="学期角色">
          <el-select v-model="memberCreateForm.role" style="width: 100%">
            <el-option label="值班人员" value="USER" />
            <el-option label="组长" value="LEADER" />
            <el-option label="负责人" value="OWNER" />
            <el-option label="人事专员" value="HR" />
            <el-option label="财务" value="FINANCE" />
          </el-select>
        </el-form-item>
        <el-form-item label="初始密码（新账户必填）">
          <el-input v-model="memberCreateForm.initialPassword" show-password autocomplete="new-password" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="memberCreateVisible = false">取消</el-button>
        <el-button type="primary" @click="submitMemberCreate">添加到本学期</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="memberEditVisible" title="编辑学期成员" width="500px">
      <el-form label-position="top">
        <el-form-item label="姓名">
          <el-input v-model="memberEditForm.realName" />
        </el-form-item>
        <el-form-item label="学期角色">
          <el-select v-model="memberEditForm.role" style="width: 100%">
            <el-option label="值班人员" value="USER" />
            <el-option label="组长" value="LEADER" />
            <el-option label="负责人" value="OWNER" />
            <el-option label="人事专员" value="HR" />
            <el-option label="财务" value="FINANCE" />
          </el-select>
        </el-form-item>
        <el-form-item label="成员排序">
          <el-input-number v-model="memberEditForm.sortOrder" :min="1" style="width: 100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="memberEditVisible = false">取消</el-button>
        <el-button type="primary" @click="saveMember">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.toolbar-actions,
.row-actions,
.semester-title {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.hidden-input {
  display: none;
}

.semester-list {
  display: grid;
  border-top: 1px solid rgba(24, 48, 66, 0.1);
}

.semester-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 20px;
  align-items: center;
  padding: 18px 4px;
  border-bottom: 1px solid rgba(24, 48, 66, 0.1);
}

.semester-row.active {
  border-left: 4px solid var(--primary);
  padding-left: 16px;
}

.semester-main p {
  margin: 6px 0 0;
}

.state {
  padding: 3px 7px;
  border-radius: 6px;
  background: #eef2f7;
  color: #52606d;
  font-size: 0.75rem;
}

.state-active {
  background: #dcfce7;
  color: #166534;
}

.state-archived {
  background: #ffedd5;
  color: #9a3412;
}

.members-band,
.settings-band {
  margin-top: 30px;
  padding-top: 24px;
  border-top: 1px solid rgba(24, 48, 66, 0.1);
}

.card-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 18px;
}

.settings-form {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  align-items: end;
}

.members-header .muted {
  margin: 6px 0 0;
}

.member-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 14px;
}

.members-table {
  width: 100%;
}

.semester-list,
.members-band,
.settings-band,
.card-header,
.member-toolbar {
  min-width: 0;
  max-width: 100%;
}

.infrastructure-note {
  margin-top: 18px;
}

@media (max-width: 900px) {
  .semester-row,
  .settings-form {
    grid-template-columns: 1fr;
  }

  .member-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .card-header,
  .page-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .toolbar-actions {
    width: 100%;
  }
}
</style>
