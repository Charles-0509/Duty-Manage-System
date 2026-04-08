<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import AvailabilityTable from '@/components/AvailabilityTable.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import {
  fetchAvailabilityOverview,
  fetchMyAvailability,
  fetchSchedule,
  fetchUserAvailability,
  saveMyAvailability,
  saveUserAvailability,
} from '@/api/services'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { buildShiftCode } from '@/utils/schedule'
import type { AvailabilityOverviewItem, ViewMode } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()

const loading = ref(false)
const saving = ref(false)
const availabilityItems = ref<AvailabilityOverviewItem[]>([])
const schedule = ref<Record<string, string[]>>({})
const viewMode = ref<ViewMode>('all')
const selectedUser = ref('')
const selectedEditableUser = ref('')
const form = reactive({
  single: [] as string[],
  double: [] as string[],
})

const isAdminEditor = computed(() => authStore.hasRole(['ADMIN']))
const scheduleFilterUser = computed(() => selectedUser.value)
const editableUsers = computed(() => availabilityItems.value)
const currentEditLabel = computed(() => {
  if (!isAdminEditor.value) return authStore.user?.realName || ''
  return editableUsers.value.find((item) => item.username === selectedEditableUser.value)?.realName || ''
})

onMounted(async () => {
  await loadPage()
})

async function loadPage() {
  loading.value = true
  try {
    await metaStore.ensureLoaded()
    const [overview, scheduleData] = await Promise.all([
      fetchAvailabilityOverview(),
      fetchSchedule(),
    ])
    availabilityItems.value = overview
    schedule.value = scheduleData

    if (isAdminEditor.value && !selectedEditableUser.value) {
      selectedEditableUser.value = overview[0]?.username || ''
    }

    await loadEditableAvailability()
  } catch {
    ElMessage.error('加载值班时间登记页面失败')
  } finally {
    loading.value = false
  }
}

async function loadEditableAvailability() {
  if (isAdminEditor.value) {
    if (!selectedEditableUser.value) {
      form.single = []
      form.double = []
      return
    }

    const payload = await fetchUserAvailability(selectedEditableUser.value)
    form.single = [...payload.single]
    form.double = [...payload.double]
    return
  }

  const payload = await fetchMyAvailability()
  form.single = [...payload.single]
  form.double = [...payload.double]
}

function toggle(shiftCode: string, mode: 'single' | 'double', checked: boolean) {
  const target = form[mode]
  const exists = target.includes(shiftCode)
  if (checked && !exists) {
    target.push(shiftCode)
  }
  if (!checked && exists) {
    form[mode] = target.filter((item) => item !== shiftCode)
  }
}

async function submit() {
  saving.value = true
  try {
    const payload = {
      single: form.single,
      double: form.double,
    }

    if (isAdminEditor.value) {
      if (!selectedEditableUser.value) {
        ElMessage.warning('请先选择成员')
        return
      }
      await saveUserAvailability(selectedEditableUser.value, payload)
      ElMessage.success('成员空闲时间已保存')
    } else {
      await saveMyAvailability(payload)
      ElMessage.success('空闲时间已保存')
    }

    await loadPage()
  } finally {
    saving.value = false
  }
}

async function handleEditableUserChange() {
  loading.value = true
  try {
    await loadEditableAvailability()
  } catch {
    ElMessage.error('加载成员空闲时间失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Availability</p>
        <h2 class="page-title">值班时间登记</h2>
        <p class="page-subtitle">
          {{
            isAdminEditor
              ? '管理员可选择成员并代为维护空闲时间，同时查看当前计划排班与全员概览。'
              : '登记你在单周和双周可值班的时间段，并同时查看当前计划排班与所有人的空闲时间总览。'
          }}
        </p>
      </div>
      <span class="pill">
        {{ isAdminEditor ? `当前编辑：${currentEditLabel || '未选择成员'}` : `当前用户：${authStore.user?.realName || ''}` }}
      </span>
    </section>

    <section class="glass-card view-card">
      <div class="view-toolbar">
        <div>
          <p class="section-label">Current Schedule</p>
          <h3>排班结果总览 (红=单周, 绿=双周, 蓝=单双周)</h3>
        </div>
        <div class="toolbar-actions">
          <el-select v-model="viewMode" placeholder="查看模式" style="width: 140px">
            <el-option label="总览" value="all" />
            <el-option label="仅单周" value="single" />
            <el-option label="仅双周" value="double" />
          </el-select>
          <el-select v-model="selectedUser" clearable placeholder="筛选某个人" style="width: 180px">
            <el-option
              v-for="name in metaStore.config?.userNames || []"
              :key="name"
              :label="name"
              :value="name"
            />
          </el-select>
        </div>
      </div>

      <ScheduleTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :schedule="schedule"
        :mode="viewMode"
        :only-user="scheduleFilterUser"
      />
    </section>

    <section class="glass-card edit-card">
      <div class="page-header">
        <div>
          <p class="section-label">Edit Availability</p>
          <h3>{{ isAdminEditor ? '成员空闲时间维护' : '我的空闲时间' }}</h3>
        </div>
        <div class="toolbar-actions">
          <el-select
            v-if="isAdminEditor"
            v-model="selectedEditableUser"
            placeholder="选择成员"
            style="width: 220px"
            @change="handleEditableUserChange"
          >
            <el-option
              v-for="item in editableUsers"
              :key="item.username"
              :label="item.realName"
              :value="item.username"
            />
          </el-select>
          <el-button type="primary" :loading="saving" @click="submit">保存登记</el-button>
        </div>
      </div>

      <div class="matrix-wrapper panel-card">
        <table class="matrix-table">
          <thead>
            <tr>
              <th>时间段</th>
              <th v-for="day in metaStore.config?.weekdaysDisplay || []" :key="day">{{ day }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(timeSlot, shiftIndex) in metaStore.config?.timeSlots || []" :key="timeSlot">
              <td>{{ timeSlot }}</td>
              <td
                v-for="dayCode in metaStore.config?.weekdaysCode || []"
                :key="`${timeSlot}-${dayCode}`"
              >
                <div class="checkbox-stack">
                  <el-checkbox
                    :model-value="form.single.includes(buildShiftCode(dayCode, shiftIndex))"
                    @change="(checked: string | number | boolean) => toggle(buildShiftCode(dayCode, shiftIndex), 'single', Boolean(checked))"
                  >
                    单周
                  </el-checkbox>
                  <el-checkbox
                    :model-value="form.double.includes(buildShiftCode(dayCode, shiftIndex))"
                    @change="(checked: string | number | boolean) => toggle(buildShiftCode(dayCode, shiftIndex), 'double', Boolean(checked))"
                  >
                    双周
                  </el-checkbox>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="glass-card">
      <div>
        <p class="section-label">Overview</p>
        <h3>所有人空闲时间总览 (红=单周, 绿=双周, 蓝=单双周)</h3>
      </div>
      <AvailabilityTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :items="availabilityItems"
      />
    </section>
  </div>
</template>

<style scoped>
.view-card,
.edit-card,
.glass-card {
  padding: 24px;
}

.view-toolbar {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 18px;
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.checkbox-stack {
  display: grid;
  gap: 8px;
}
</style>
