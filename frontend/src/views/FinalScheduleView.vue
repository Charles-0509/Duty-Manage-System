<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchFinalSchedule, saveFinalSchedule } from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import { buildShiftCode, calculateWeekNumber } from '@/utils/schedule'

const metaStore = useMetaStore()
const loading = ref(false)
const saving = ref(false)
const selectedDate = ref(dayjs().format('YYYY-MM-DD'))
const source = ref<'saved' | 'generated'>('generated')
const schedule = ref<Record<string, string[]>>({})

const weekNumber = computed(() =>
  metaStore.config ? calculateWeekNumber(selectedDate.value, metaStore.config.firstMonday) : 1,
)

watch(weekNumber, async () => {
  if (metaStore.config) {
    await loadSchedule()
  }
})

onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadSchedule()
})

async function loadSchedule() {
  loading.value = true
  try {
    const response = await fetchFinalSchedule(weekNumber.value, selectedDate.value)
    schedule.value = response.schedule
    source.value = response.source
  } catch {
    ElMessage.error('加载实际值班表失败')
  } finally {
    loading.value = false
  }
}

async function persist() {
  saving.value = true
  try {
    await saveFinalSchedule(weekNumber.value, {
      selectedDate: selectedDate.value,
      schedule: schedule.value,
    })
    ElMessage.success('实际值班表已保存')
    await loadSchedule()
  } finally {
    saving.value = false
  }
}

function clearTable() {
  schedule.value = {}
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Actual Shift</p>
        <h2 class="page-title">实际值班表调整</h2>
        <p class="page-subtitle">
          选择本周任意一天，系统会根据单双周模板自动带出计划排班；你可以据此调整成实际值班结果并保存。
        </p>
      </div>
      <div class="toolbar-actions">
        <el-date-picker v-model="selectedDate" value-format="YYYY-MM-DD" type="date" placeholder="选择日期" />
        <el-button @click="clearTable">清空表格</el-button>
        <el-button type="primary" :loading="saving" @click="persist">保存实际值班表</el-button>
      </div>
    </section>

    <section class="data-grid">
      <article class="glass-card stat-box">
        <p class="section-label">Week</p>
        <h3>第 {{ weekNumber }} 周</h3>
        <p class="muted">按学期第一周周一推算。</p>
      </article>
      <article class="glass-card stat-box">
        <p class="section-label">Source</p>
        <h3>{{ source === 'saved' ? '已保存数据' : '根据计划自动生成' }}</h3>
        <p class="muted">保存后再次进入会优先读取历史实际值班表。</p>
      </article>
    </section>

    <section class="glass-card">
      <div>
        <p class="section-label">Editor</p>
        <h3>调整后的实际值班表</h3>
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
                  placeholder="选择实际值班人员"
                  style="width: 100%"
                >
                  <el-option
                    v-for="name in metaStore.config?.userNames || []"
                    :key="name"
                    :label="name"
                    :value="name"
                  />
                </el-select>
              </td>
            </tr>
          </tbody>
        </table>
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

.stat-box h3 {
  margin: 10px 0 6px;
}
</style>
