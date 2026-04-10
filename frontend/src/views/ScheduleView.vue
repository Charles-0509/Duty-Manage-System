<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import AvailabilityTable from '@/components/AvailabilityTable.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import { downloadScheduleWorkbook, fetchAvailabilityOverview, fetchScheduleSummary, saveSchedule } from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import { buildShiftCode, hasAvailability, downloadBlob } from '@/utils/schedule'
import type { AvailabilityOverviewItem, DashboardChartItem, ViewMode } from '@/types'

const metaStore = useMetaStore()
const loading = ref(false)
const saving = ref(false)
const availabilityItems = ref<AvailabilityOverviewItem[]>([])
const schedule = ref<Record<string, string[]>>({})
const shiftStats = ref<DashboardChartItem[]>([])
const viewMode = ref<ViewMode>('all')

onMounted(async () => {
  await loadPage()
})

async function loadPage() {
  loading.value = true
  try {
    await metaStore.ensureLoaded()
    const [overview, scheduleData] = await Promise.all([fetchAvailabilityOverview(), fetchScheduleSummary()])
    availabilityItems.value = overview
    schedule.value = { ...scheduleData.schedule }
    shiftStats.value = scheduleData.shiftDistribution
  } catch {
    ElMessage.error('加载管理员排班页面失败')
  } finally {
    loading.value = false
  }
}

function shiftOptions(dayCode: string, shiftIndex: number) {
  const code = buildShiftCode(dayCode, shiftIndex)
  return availabilityItems.value
    .map((item: AvailabilityOverviewItem) => {
      const single = hasAvailability(item.availability, code, 'single')
      const double = hasAvailability(item.availability, code, 'double')
      if (!single && !double) return null
      if (single && double) return `${item.realName}(单双)`
      if (single) return `${item.realName}(单)`
      return `${item.realName}(双)`
    })
    .filter(Boolean) as string[]
}

async function persist() {
  saving.value = true
  try {
    await saveSchedule(schedule.value)
    ElMessage.success('排班已保存')
    await loadPage()
  } finally {
    saving.value = false
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
            <el-button type="primary" :loading="saving" @click="persist">保存排班</el-button>
            <el-button @click="exportExcel">导出 Excel</el-button>
          </div>
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
              <td v-for="dayCode in metaStore.config?.weekdaysCode || []" :key="`${timeSlot}-${dayCode}`">
                <el-select
                  v-model="schedule[buildShiftCode(dayCode, shiftIndex)]"
                  multiple
                  filterable
                  collapse-tags
                  collapse-tags-tooltip
                  placeholder="选择人员"
                  style="width: 100%"
                >
                  <el-option
                    v-for="option in shiftOptions(dayCode, shiftIndex)"
                    :key="option"
                    :label="option"
                    :value="option"
                  />
                </el-select>
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
</style>

