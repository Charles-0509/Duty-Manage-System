<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { fetchDashboard } from '@/api/services'
import MetricCard from '@/components/MetricCard.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import { useMetaStore } from '@/stores/meta'
import type { DashboardData } from '@/types'

use([CanvasRenderer, BarChart, PieChart, GridComponent, TooltipComponent, LegendComponent])

const metaStore = useMetaStore()
const loading = ref(false)
const dashboard = ref<DashboardData | null>(null)

const shiftOption = computed(() => ({
  tooltip: { trigger: 'axis' },
  grid: { left: 36, right: 24, top: 28, bottom: 24 },
  xAxis: {
    type: 'category',
    data: dashboard.value?.shiftDistribution.map((item: { name: string }) => item.name) || [],
    axisLabel: { interval: 0, rotate: 18 },
  },
  yAxis: { type: 'value' },
  series: [
    {
      type: 'bar',
      barWidth: 28,
      data: dashboard.value?.shiftDistribution.map((item: { value: number }) => item.value) || [],
      itemStyle: { color: '#0f766e', borderRadius: [10, 10, 0, 0] },
    },
  ],
}))

const workShareOption = computed(() => ({
  tooltip: { trigger: 'item' },
  legend: { bottom: 0 },
  series: [
    {
      type: 'pie',
      radius: ['42%', '72%'],
      data: dashboard.value?.workDurationShare || [],
      itemStyle: {
        borderColor: '#fffaf2',
        borderWidth: 3,
      },
    },
  ],
}))

onMounted(async () => {
  loading.value = true
  try {
    await metaStore.ensureLoaded()
    dashboard.value = await fetchDashboard()
  } catch {
    ElMessage.error('加载仪表盘失败')
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page-shell">
    <section class="page-header">
      <div>
        <p class="section-label">Overview</p>
        <h2 class="page-title">值班总览</h2>
        <p class="page-subtitle">
          首页集中展示当前排班结果、值班登记进度和工单工时分布，方便你先看整体，再进入具体页面处理。
        </p>
      </div>
      <span class="pill">实时读取后端 SQLite 数据</span>
    </section>

    <section class="page-shell">
      <div>
        <p class="section-label">Current Schedule</p>
        <h3>当前计划排班 (红=单周, 绿=双周, 蓝=单双周)</h3>
      </div>
      <ScheduleTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :schedule="dashboard?.schedule || {}"
      />
    </section>

    <section v-loading="loading" class="data-grid">
      <MetricCard label="已登记空闲时间人数" :value="dashboard?.availabilityUserCount || 0" accent="#0f766e" />
      <MetricCard label="总排班人次" :value="dashboard?.totalAssignedShifts || 0" accent="#f97316" />
      <MetricCard label="工单总数" :value="dashboard?.workOrderCount || 0" accent="#2563eb" />
    </section>

    <section class="split-layout">
      <article class="glass-card chart-card">
        <div class="card-top">
          <div>
            <p class="section-label">排班统计</p>
            <h3>各人员排班班次分布</h3>
          </div>
        </div>
        <v-chart v-if="dashboard?.shiftDistribution.length" class="chart" :option="shiftOption" autoresize />
        <el-empty v-else description="暂无排班统计" />
      </article>

      <article class="glass-card chart-card">
        <div class="card-top">
          <div>
            <p class="section-label">工单工时</p>
            <h3>人员工单时长占比</h3>
          </div>
        </div>
        <v-chart v-if="dashboard?.workDurationShare.length" class="chart" :option="workShareOption" autoresize />
        <el-empty v-else description="暂无工单时长数据" />
      </article>
    </section>
  </div>
</template>

<style scoped>
.chart-card {
  padding: 24px;
}

.card-top h3 {
  margin: 8px 0 0;
}

.chart {
  height: 360px;
}
</style>
