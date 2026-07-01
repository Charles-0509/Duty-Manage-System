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

const canViewWorkDurationShare = computed(() => authStore.hasRole(['ADMIN', 'OWNER']))
const maxShiftValue = computed(() => Math.max(...(dashboard.value?.shiftDistribution.map((item) => item.value) || [0]), 1))
const maxWorkValue = computed(() => Math.max(...(dashboard.value?.workDurationShare.map((item) => item.value) || [0]), 1))

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

    <section class="charts-stack">
      <article class="glass-card chart-card">
        <div class="card-top">
          <div>
            <p class="section-label">排班统计</p>
            <h3>各人员排班班次分布</h3>
          </div>
        </div>
        <div v-if="dashboard?.shiftDistribution.length" class="bar-chart">
          <div v-for="item in dashboard.shiftDistribution" :key="item.name" class="bar-item">
            <div class="bar-track">
              <span class="bar-fill" :style="{ height: `${Math.max((item.value / maxShiftValue) * 100, 8)}%` }" />
            </div>
            <strong>{{ item.value }}</strong>
            <span>{{ item.name }}</span>
          </div>
        </div>
        <el-empty v-else description="暂无排班统计" />
      </article>

      <article v-if="canViewWorkDurationShare" class="glass-card chart-card">
        <div class="card-top">
          <div>
            <p class="section-label">工单工时</p>
            <h3>人员工单时长占比</h3>
          </div>
        </div>
        <div v-if="dashboard?.workDurationShare.length" class="share-list">
          <div v-for="item in dashboard.workDurationShare" :key="item.name" class="share-row">
            <span>{{ item.name }}</span>
            <div class="share-track">
              <span class="share-fill" :style="{ width: `${Math.max((item.value / maxWorkValue) * 100, 4)}%` }" />
            </div>
            <strong>{{ Number(item.value).toFixed(1) }}h</strong>
          </div>
        </div>
        <el-empty v-else description="暂无工单时长数据" />
      </article>
    </section>
  </div>
</template>

<style scoped>
.charts-stack {
  display: grid;
  gap: 22px;
}

.chart-card {
  padding: 24px;
}

.card-top h3 {
  margin: 8px 0 0;
}

.bar-chart {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(70px, 1fr));
  gap: 18px;
  align-items: end;
  min-height: 360px;
}

.bar-item {
  display: grid;
  gap: 8px;
  justify-items: center;
  min-width: 0;
  color: var(--muted);
  font-size: 0.88rem;
}

.bar-item strong {
  color: var(--text);
}

.bar-track {
  position: relative;
  width: 100%;
  max-width: 52px;
  height: 250px;
  border-radius: 8px;
  background: rgba(15, 118, 110, 0.08);
  overflow: hidden;
}

.bar-fill {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  border-radius: 8px 8px 0 0;
  background: linear-gradient(180deg, #14b8a6, #0f766e);
}

.share-list {
  display: grid;
  gap: 16px;
  min-height: 360px;
  align-content: center;
}

.share-row {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr) 72px;
  gap: 16px;
  align-items: center;
}

.share-row span {
  min-width: 0;
  color: var(--text);
}

.share-row strong {
  text-align: right;
}

.share-track {
  height: 14px;
  border-radius: 999px;
  background: rgba(37, 99, 235, 0.08);
  overflow: hidden;
}

.share-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #2563eb, #0f766e);
}
</style>
