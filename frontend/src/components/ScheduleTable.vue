<script setup lang="ts">
import { computed } from 'vue'
import { useAutoScaleTable } from '@/composables/useAutoScaleTable'
import { baseName, tagType, visibleScheduleNames } from '@/utils/schedule'
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

function shiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}

function renderItems(dayCode: string, shiftIndex: number) {
  return visibleScheduleNames(props.schedule[shiftCode(dayCode, shiftIndex)] || [], props.mode, props.onlyUser)
}
</script>

<template>
  <div :ref="autoScale.containerRef" class="matrix-wrapper panel-card" :class="{ 'matrix-wrapper--scaled': isScaled }">
    <div class="matrix-scale-shell" :style="shellStyle">
      <table :ref="autoScale.tableRef" class="matrix-table" :style="tableStyle">
        <thead>
          <tr>
            <th>时间段</th>
            <th v-for="(day, index) in weekdaysDisplay" :key="weekdaysCode[index]">{{ day }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(timeSlot, shiftIndex) in timeSlots" :key="timeSlot">
            <td>{{ timeSlot }}</td>
            <td v-for="dayCode in weekdaysCode" :key="`${timeSlot}-${dayCode}`">
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
              <span v-else class="muted">-</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
