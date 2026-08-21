<script setup lang="ts">
import { computed } from 'vue'
import { useAutoScaleTable } from '@/composables/useAutoScaleTable'
import { baseName, buildVisibleScheduleByCode, tagType } from '@/utils/schedule'
import type { ViewMode } from '@/types'

const props = withDefaults(
  defineProps<{
    weekdaysCode: string[]
    weekdaysDisplay: string[]
    timeSlots: string[]
    schedule: Record<string, string[]>
    mode?: ViewMode
    onlyUser?: string
  }>(),
  {
    mode: 'all',
    onlyUser: '',
  },
)

const autoScale = useAutoScaleTable()
const isScaled = computed(() => autoScale.scale.value < 1)
const shellStyle = computed(() => autoScale.shellStyle.value)
const tableStyle = computed(() => autoScale.tableStyle.value)
const visibleSchedule = computed(() => buildVisibleScheduleByCode(props.schedule, props.mode, props.onlyUser))

function shiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}

function renderItems(dayCode: string, shiftIndex: number) {
  return visibleSchedule.value[shiftCode(dayCode, shiftIndex)] || []
}
</script>

<template>
  <div :ref="autoScale.containerRef" class="matrix-wrapper panel-card" :class="{ 'matrix-wrapper--scaled': isScaled }">
    <div class="matrix-scale-shell" :style="shellStyle">
      <table :ref="autoScale.tableRef" class="matrix-table" :style="tableStyle">
        <thead>
          <tr>
            <th class="matrix-header-first">时段 / 星期</th>
            <th v-for="(day, index) in weekdaysDisplay" :key="weekdaysCode[index]">
              <div class="header-day">{{ day }}</div>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(timeSlot, shiftIndex) in timeSlots" :key="timeSlot">
            <td class="matrix-cell-time">{{ timeSlot }}</td>
            <td v-for="dayCode in weekdaysCode" :key="`${timeSlot}-${dayCode}`" class="matrix-cell-content">
              <template v-if="renderItems(dayCode, shiftIndex).length">
                <span
                  v-for="label in renderItems(dayCode, shiftIndex)"
                  :key="label"
                  class="name-chip"
                  :class="tagType(label)"
                >
                  {{ baseName(label) }}
                </span>
              </template>
              <span v-else class="empty-cell">-</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.matrix-header-first {
  font-size: 0.82rem;
  color: var(--muted);
}

.header-day {
  font-weight: 600;
  font-size: 0.88rem;
}

.matrix-cell-time {
  font-size: 0.82rem;
  font-weight: 600;
}

.matrix-cell-content {
  min-height: 48px;
}

.empty-cell {
  color: #cbd5e1;
  font-weight: 500;
}
</style>
