<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import AvailabilityTable from '@/components/AvailabilityTable.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import { autoGenerateSchedule, downloadScheduleWorkbook, fetchAvailabilityOverview, fetchScheduleSummary, saveSchedule } from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import { buildShiftCode, buildShiftOptionsByCode, downloadBlob, normalizeScheduleLabels } from '@/utils/schedule'
import type { AvailabilityOverviewItem, DashboardChartItem, ViewMode } from '@/types'

const metaStore = useMetaStore()
const loading = ref(false)
const saving = ref(false)
const availabilityItems = ref<AvailabilityOverviewItem[]>([])
const schedule = ref<Record<string, string[]>>({})
const shiftStats = ref<DashboardChartItem[]>([])
const viewMode = ref<ViewMode>('all')
const autoDialogVisible = ref(false)
const autoGenerating = ref(false)
const autoWarnings = ref<string[]>([])
const autoForm = reactive({ perSlot: 1, preserveExisting: true })
const shiftOptionsByCode = computed(() => buildShiftOptionsByCode(availabilityItems.value))

onMounted(async () => {
  await loadPage()
})

async function loadPage() {
  loading.value = true
  try {
    await metaStore.ensureLoaded()
    const [overview, scheduleData] = await Promise.all([fetchAvailabilityOverview(), fetchScheduleSummary()])
    availabilityItems.value = overview
    schedule.value = Object.fromEntries(
      Object.entries(scheduleData.schedule).map(([shiftCode, labels]) => [shiftCode, normalizeScheduleLabels(labels)]),
    )
    shiftStats.value = scheduleData.shiftDistribution
  } catch {
    ElMessage.error('加载管理员排班页面失败')
  } finally {
    loading.value = false
  }
}

async function persist() {
  saving.value = true
  try {
    await saveSchedule(schedule.value)
    ElMessage.success('排班已保存')
    await loadPage()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '保存排班失败')
  } finally {
    saving.value = false
  }
}

async function runAutoSchedule() {
  autoGenerating.value = true
  try {
    const result = await autoGenerateSchedule(
      autoForm.perSlot,
      autoForm.preserveExisting ? schedule.value : {},
    )
    schedule.value = Object.fromEntries(
      Object.entries(result.schedule).map(([shiftCode, labels]) => [shiftCode, normalizeScheduleLabels(labels)]),
    )
    shiftStats.value = result.shiftDistribution
    autoWarnings.value = result.warnings || []
    autoDialogVisible.value = false
    ElMessage.success('已按空闲时间生成排班建议，可直接在下方手动调整后保存')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '自动排班失败')
  } finally {
    autoGenerating.value = false
  }
}

async function exportExcel() {
  try {
    const blob = await downloadScheduleWorkbook()
    downloadBlob(blob, '排班表.xlsx')
  } catch {
    ElMessage.error('导出排班失败')
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Schedule</p>
        <h2 class="page-title">管理员排班</h2>
        <p class="page-subtitle">
          先查看全员空闲时间，再在每个班次里直接选择可排成员，保存后即可导出计划排班表。
        </p>
      </div>
    </section>

    <section class="glass-card">
      <div>
        <p class="section-label">Availability</p>
        <h3>当前所有人空闲时间 (红=单周, 绿=双周, 蓝=单双周)</h3>
      </div>
      <AvailabilityTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :items="availabilityItems"
      />
    </section>

    <section class="glass-card stat-card">
      <div class="page-header">
        <div>
          <p class="section-label">Result</p>
          <h3>排班结果预览 (红=单周, 绿=双周, 蓝=单双周)</h3>
        </div>
        <el-select v-model="viewMode" style="width: 140px">
          <el-option label="总览" value="all" />
          <el-option label="仅单周" value="single" />
          <el-option label="仅双周" value="double" />
        </el-select>
      </div>
      <ScheduleTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :schedule="schedule"
        :mode="viewMode"
      />
    </section>

    <section class="glass-card">
      <div class="page-header">
        <div>
          <p class="section-label">Editor</p>
          <h3>手动排班</h3>
        </div>
        <div class="editor-actions">
          <span class="pill editor-hint">仅显示当前班次可排人员</span>
          <div class="toolbar-actions">
            <el-button @click="autoDialogVisible = true">自动排班</el-button>
            <el-button type="primary" :loading="saving" @click="persist">保存排班</el-button>
            <el-button @click="exportExcel">导出 Excel</el-button>
          </div>
        </div>
      </div>

      <el-alert
        v-if="autoWarnings.length"
        type="warning"
        show-icon
        :closable="true"
        title="自动排班提示"
        style="margin-bottom: 14px"
      >
        <div v-for="warning in autoWarnings" :key="warning">{{ warning }}</div>
      </el-alert>

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
              <td v-for="dayCode in metaStore.config?.weekdaysCode || []" :key="`${timeSlot}-${dayCode}`">
                <el-select-v2
                  class="editor-member-select"
                  v-model="schedule[buildShiftCode(dayCode, shiftIndex)]"
                  :options="shiftOptionsByCode[buildShiftCode(dayCode, shiftIndex)] || []"
                  multiple
                  filterable
                  placeholder="选择人员"
                  style="width: 100%"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="glass-card stat-card">
      <p class="section-label">统计</p>
      <h3>排班班次统计</h3>
      <el-empty v-if="!shiftStats.length" description="暂无排班数据" />
      <div v-else class="stat-list">
        <div v-for="item in shiftStats" :key="item.name" class="stat-row">
          <span>{{ item.name }}</span>
          <strong>{{ item.value }} 班</strong>
        </div>
      </div>
    </section>
    <el-dialog v-model="autoDialogVisible" title="按空闲时间自动排班" width="440px">
      <p class="muted" style="margin-top: 0">
        系统会按已登记的单双周空闲时间补齐排班建议，优先保留单双周组合并均衡每个人的班次。
      </p>
      <el-form label-position="top">
        <el-form-item label="每班人数">
          <el-radio-group v-model="autoForm.perSlot">
            <el-radio-button :value="1">1 人</el-radio-button>
            <el-radio-button :value="2">2 人</el-radio-button>
            <el-radio-button :value="3">3 人</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="autoForm.preserveExisting">保留当前已选人员，只自动补齐空缺</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="autoDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="autoGenerating" @click="runAutoSchedule">生成排班建议</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.glass-card {
  padding: 24px;
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.editor-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
  flex-wrap: wrap;
}

.editor-hint {
  margin-bottom: 4px;
}

.stat-card {
  display: grid;
  gap: 18px;
}

.stat-list {
  display: grid;
  gap: 10px;
}

.stat-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid var(--line);
}

@media (max-width: 900px) {
  .editor-actions {
    width: 100%;
    justify-content: flex-start;
  }
}

:deep(.editor-member-select .el-select__wrapper) {
  min-height: 42px;
  height: auto;
  align-items: flex-start;
  padding-top: 6px;
  padding-bottom: 6px;
}

:deep(.editor-member-select .el-select__selection) {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 6px;
}

:deep(.editor-member-select .el-select__selected-item) {
  max-width: 100%;
}

:deep(.editor-member-select .el-tag) {
  margin: 0;
  max-width: 100%;
  height: auto;
  white-space: normal;
  line-height: 1.35;
  padding-top: 4px;
  padding-bottom: 4px;
}
</style>
