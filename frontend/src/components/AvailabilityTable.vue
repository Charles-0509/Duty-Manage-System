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
const isScrollMode = computed(() => autoScale.isScrollMode.value)
const shellStyle = computed(() => autoScale.shellStyle.value)
const tableStyle = computed(() => autoScale.tableStyle.value)
const availabilityCells = computed(() => buildAvailabilityCells(props.items, props.mode))

function shiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}


function formatTimeSlot(slot: string) {
  if (!slot) return { start: '', end: '' }
  const parts = slot.split('-')
  if (parts.length === 2) {
    return { start: parts[0], end: parts[1] }
  }
  return { start: slot, end: '' }
}

function users(dayCode: string, shiftIndex: number) {
  return availabilityCells.value[shiftCode(dayCode, shiftIndex)] || []
}
</script>

<template>
  <div :ref="autoScale.containerRef" class="matrix-wrapper panel-card" :class="{ 'matrix-wrapper--scaled': isScaled, 'matrix-wrapper--scroll-mode': isScrollMode }">
    <div class="matrix-scale-shell" :style="shellStyle">
      <table :ref="autoScale.tableRef" class="matrix-table" :style="tableStyle">
        <thead>
          <tr>
            <th class="matrix-header-first">时段</th>
            <th v-for="(day, index) in weekdaysDisplay" :key="weekdaysCode[index]">
              <div class="header-day">{{ day }}</div>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(timeSlot, shiftIndex) in timeSlots" :key="timeSlot">
            <td class="matrix-cell-time">
              <div class="time-slot-wrap">
                <span class="time-slot-start">{{ formatTimeSlot(timeSlot).start }}-</span>
                <span class="time-slot-end">{{ formatTimeSlot(timeSlot).end }}</span>
              </div>
            </td>
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
  text-align: center;
}

.header-day {
  font-weight: 600;
  font-size: 0.88rem;
}

.matrix-cell-time {
  font-size: 0.82rem;
  font-weight: 600;
  text-align: center;
}

.time-slot-wrap {
  display: flex;
  flex-direction: row;
  align-items: center;
  justify-content: center;
  line-height: 1.35;
  white-space: nowrap;
  gap: 1px;
}

/* In scroll/narrow mode, time slot strictly wraps into 2 clean centered lines */
.matrix-wrapper--scroll-mode .time-slot-wrap {
  flex-direction: column;
  gap: 0;
  line-height: 1.2;
}

.matrix-cell-content {
  min-height: 48px;
}

.empty-cell {
  color: #cbd5e1;
  font-weight: 500;
}
</style>
