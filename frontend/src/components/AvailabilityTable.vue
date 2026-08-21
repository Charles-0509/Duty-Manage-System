<script setup lang="ts">
import { computed } from 'vue'
import { useAutoScaleTable } from '@/composables/useAutoScaleTable'
import type { AvailabilityOverviewItem, ViewMode } from '@/types'
import { buildAvailabilityCells } from '@/utils/schedule'

const props = withDefaults(
  defineProps<{
    weekdaysCode: string[]
    weekdaysDisplay: string[]
    timeSlots: string[]
    items: AvailabilityOverviewItem[]
    mode?: ViewMode
  }>(),
  {
    mode: 'all',
  },
)

const autoScale = useAutoScaleTable()
const isScaled = computed(() => autoScale.scale.value < 1)
const shellStyle = computed(() => autoScale.shellStyle.value)
const tableStyle = computed(() => autoScale.tableStyle.value)
const availabilityCells = computed(() => buildAvailabilityCells(props.items, props.mode))

function shiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}

function users(dayCode: string, shiftIndex: number) {
  return availabilityCells.value[shiftCode(dayCode, shiftIndex)] || []
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
              <div v-if="users(dayCode, shiftIndex).length" class="name-chip-list">
                <span
                  v-for="item in users(dayCode, shiftIndex)"
                  :key="`${dayCode}-${shiftIndex}-${item.name}`"
                  class="name-chip"
                  :class="item.tone"
                >
                  {{ item.name }}
                </span>
              </div>
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
