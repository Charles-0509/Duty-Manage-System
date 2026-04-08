<script setup lang="ts">
import type { AvailabilityOverviewItem, ViewMode } from '@/types'
import { availabilityCellUsers } from '@/utils/schedule'

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

function shiftCode(dayCode: string, shiftIndex: number) {
  return `${dayCode}-${shiftIndex + 1}`
}

function users(dayCode: string, shiftIndex: number) {
  return availabilityCellUsers(props.items, shiftCode(dayCode, shiftIndex), props.mode)
}
</script>

<template>
  <div class="matrix-wrapper panel-card">
    <table class="matrix-table">
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
            <template v-if="users(dayCode, shiftIndex).length">
              <span
                v-for="item in users(dayCode, shiftIndex)"
                :key="`${dayCode}-${shiftIndex}-${item.name}`"
                class="name-chip"
                :class="item.tone"
              >
                {{ item.name }}
              </span>
            </template>
            <span v-else class="muted">-</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
