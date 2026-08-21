<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchDashboard } from '@/api/services'
import MetricCard from '@/components/MetricCard.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import type { DashboardData } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()
const loading = ref(false)
const dashboard = ref<DashboardData | null>(null)

const canViewWorkOrderStats = computed(() => authStore.can('view_workorders'))
const maxShiftValue = computed(() => Math.max(...(dashboard.value?.shiftDistribution.map((item) => item.value) || [0]), 1))
const maxWorkValue = computed(() => Math.max(...(dashboard.value?.workDurationShare.map((item) => item.value) || [0]), 1))

// 排序与平均值计算
const avgWorkHours = computed(() => {
  const list = dashboard.value?.workDurationShare || []
  if (!list.length) return 0
  const total = list.reduce((sum, item) => sum + item.value, 0)
  return total / list.length
})

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
          集中查看当前排班结果、值班登记进度与工单时长统计。
        </p>
      </div>
    </section>

    <!-- 核心指标卡片 -->
    <section v-loading="loading" class="data-grid">
      <MetricCard
        label="已登记空闲时间人数"
        :value="dashboard?.availabilityUserCount || 0"
        accent="#0d9488"
        subtext="当前学期已提交时间意向"
      />
      <MetricCard
        label="总排班人次"
        :value="dashboard?.totalAssignedShifts || 0"
        accent="#f59e0b"
        subtext="已落定计划排班班次"
      />
      <MetricCard
        v-if="canViewWorkOrderStats"
        label="工单总数"
        :value="dashboard?.workOrderCount || 0"
        accent="#3b82f6"
        subtext="本周期登记处理工单"
      />
    </section>

    <!-- 当前计划排班 -->
    <section class="panel-card schedule-section">
      <div class="section-top">
        <div class="section-title-wrap">
          <p class="section-label">Schedule Matrix</p>
          <h3 class="section-heading">当前计划排班</h3>
        </div>
        <div class="legend-group">
          <span class="name-chip single">单周</span>
          <span class="name-chip double">双周</span>
          <span class="name-chip both">单双周</span>
        </div>
      </div>
      <ScheduleTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :schedule="dashboard?.schedule || {}"
      />
    </section>

    <!-- 统计图表区 -->
    <section class="charts-stack">
      <article class="panel-card chart-card">
        <div class="card-top">
          <div>
            <p class="section-label">排班统计</p>
            <h3 class="chart-heading">各人员排班班次分布</h3>
          </div>
        </div>
        <div v-if="dashboard?.shiftDistribution.length" class="bar-chart-container">
          <div class="bar-chart">
            <div v-for="item in dashboard.shiftDistribution" :key="item.name" class="bar-item">
              <div class="bar-track">
                <div
                  class="bar-fill"
                  :style="{ height: `${Math.max((item.value / maxShiftValue) * 100, 6)}%` }"
                >
                  <span class="bar-tooltip">{{ item.value }} 班</span>
                </div>
              </div>
              <strong class="bar-val">{{ item.value }}</strong>
              <span class="bar-label" :title="item.name">{{ item.name }}</span>
            </div>
          </div>
        </div>
        <el-empty v-else description="暂无排班统计" />
      </article>

      <article v-if="canViewWorkOrderStats" class="panel-card chart-card">
        <div class="card-top flex-between">
          <div>
            <p class="section-label">工单工时</p>
            <h3 class="chart-heading">人员工单时长分布</h3>
          </div>
          <div v-if="avgWorkHours > 0" class="avg-tag">
            平均工时: <strong>{{ avgWorkHours.toFixed(1) }}h</strong>
          </div>
        </div>
        <div v-if="dashboard?.workDurationShare.length" class="share-list">
          <div v-for="item in dashboard.workDurationShare" :key="item.name" class="share-row">
            <span class="share-name" :title="item.name">{{ item.name }}</span>
            <div class="share-track">
              <span
                class="share-fill"
                :style="{ width: `${Math.max((item.value / maxWorkValue) * 100, 3)}%` }"
              />
            </div>
            <strong class="share-value">{{ Number(item.value).toFixed(1) }}h</strong>
          </div>
        </div>
        <el-empty v-else description="暂无工单时长数据" />
      </article>
    </section>
  </div>
</template>

<style scoped>
.schedule-section {
  padding: 20px;
}

.section-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 12px;
}

.section-heading {
  margin: 2px 0 0;
  font-size: 1.15rem;
  font-weight: 600;
  color: #0f172a;
}

.legend-group {
  display: flex;
  align-items: center;
  gap: 4px;
}

.legend-group .name-chip {
  margin: 0;
}

.charts-stack {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
  gap: 18px;
}

.chart-card {
  padding: 20px;
  background: #ffffff;
}

.chart-heading {
  margin: 2px 0 0;
  font-size: 1.1rem;
  font-weight: 600;
  color: #0f172a;
}

.flex-between {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.avg-tag {
  font-size: 0.82rem;
  color: var(--muted);
  background: #f1f5f9;
  padding: 4px 10px;
  border-radius: var(--radius-sm);
}

.avg-tag strong {
  color: #0f172a;
}

.bar-chart-container {
  overflow-x: auto;
  padding-top: 24px;
  padding-bottom: 8px;
}

.bar-chart {
  display: flex;
  gap: 16px;
  align-items: flex-end;
  min-height: 220px;
  min-width: max-content;
  padding: 0 8px;
}

.bar-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  width: 44px;
  color: var(--muted);
  font-size: 0.82rem;
}

.bar-item strong {
  color: #0f172a;
  font-size: 0.88rem;
  font-variant-numeric: tabular-nums;
}

.bar-label {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 50px;
  text-align: center;
}

.bar-track {
  position: relative;
  width: 18px;
  height: 150px;
  border-radius: 4px;
  background: #f1f5f9;
  display: flex;
  align-items: flex-end;
}

.bar-fill {
  position: relative;
  width: 100%;
  border-radius: 4px 4px 0 0;
  background: linear-gradient(180deg, #14b8a6, #0d9488);
  transition: height 0.3s ease;
  cursor: pointer;
}

.bar-fill:hover {
  background: linear-gradient(180deg, #2dd4bf, #0f766e);
}

.bar-tooltip {
  display: none;
  position: absolute;
  top: -28px;
  left: 50%;
  transform: translateX(-50%);
  background: #0f172a;
  color: #ffffff;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 0.72rem;
  white-space: nowrap;
  pointer-events: none;
}

.bar-fill:hover .bar-tooltip {
  display: block;
}

.share-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 18px;
  max-height: 280px;
  overflow-y: auto;
}

.share-row {
  display: grid;
  grid-template-columns: 80px minmax(0, 1fr) 56px;
  gap: 12px;
  align-items: center;
  font-size: 0.88rem;
}

.share-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: #334155;
  font-weight: 500;
}

.share-value {
  text-align: right;
  font-variant-numeric: tabular-nums;
  color: #0f172a;
}

.share-track {
  height: 8px;
  border-radius: 999px;
  background: #f1f5f9;
  overflow: hidden;
}

.share-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #3b82f6, #0d9488);
}

@media (max-width: 768px) {
  .charts-stack {
    grid-template-columns: 1fr;
  }
}
</style>
