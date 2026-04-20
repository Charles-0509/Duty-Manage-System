<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { downloadFinanceWorkbook, fetchFinanceSummary } from '@/api/services'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { defaultMonthOption, downloadBlob, monthOptions } from '@/utils/schedule'
import type { FinanceSummary } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()

const loading = ref(false)
const exporting = ref(false)
const selectedMonth = ref(defaultMonthOption())
const selectedMember = ref('')

const summary = ref<FinanceSummary>({
  month: selectedMonth.value,
  dutyHours: 0,
  dutyAmount: 0,
  workOrderHours: 0,
  workOrderAmount: 0,
  managementAmount: 0,
  managementPending: false,
  totalAmount: 0,
  workOrderDetails: [],
})

const canExport = computed(() => authStore.hasRole(['ADMIN', 'OWNER']))
const canSelectMember = computed(() => authStore.hasRole(['ADMIN', 'OWNER']))
const currentMemberName = computed(() => selectedMember.value || authStore.user?.realName || '')
const showManagementCard = computed(() => summary.value.managementPending || summary.value.managementAmount > 0)

const monthlyCards = computed(() => {
  const cards = [
    {
      label: '值班酬劳',
      value: formatCurrency(summary.value.dutyAmount),
      note: `${summary.value.dutyHours.toFixed(1)} 小时 × 25 元/小时`,
    },
    {
      label: '工单酬劳',
      value: formatCurrency(summary.value.workOrderAmount),
      note: `${summary.value.workOrderHours.toFixed(1)} 小时 × 50 元/小时`,
    },
  ]

  if (showManagementCard.value) {
    cards.push({
      label: '项目管理薪酬',
      value: summary.value.managementPending ? '未计算' : formatCurrency(summary.value.managementAmount),
      note: summary.value.managementPending
        ? '未来月份的项目管理薪酬会在到达当月后再计算'
        : summary.value.managementAmount >= 1200
          ? '负责人固定每月 1200 元'
          : '组长固定每月 800 元',
    })
  }

  cards.push({
    label: '总酬劳',
    value: formatCurrency(summary.value.totalAmount),
    note: '值班、工单和项目管理薪酬直接累加',
  })

  return cards
})

watch(selectedMonth, async () => {
  await loadSummary()
})

watch(selectedMember, async () => {
  await loadSummary()
})

onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadSummary()
})

async function loadSummary() {
  loading.value = true
  try {
    summary.value = await fetchFinanceSummary(selectedMonth.value, selectedMember.value)
  } catch {
    ElMessage.error('加载财务统计失败')
  } finally {
    loading.value = false
  }
}

async function exportExcel() {
  exporting.value = true
  try {
    const blob = await downloadFinanceWorkbook(selectedMonth.value)
    downloadBlob(blob, `${selectedMonth.value}-财务统计.xlsx`)
  } catch {
    ElMessage.error('导出财务统计失败')
  } finally {
    exporting.value = false
  }
}

function formatCurrency(amount: number) {
  return `￥ ${amount.toFixed(2)}`
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Finance</p>
        <h2 class="page-title">财务统计</h2>
        <p class="page-subtitle">估算成员月度酬劳，并在页面下方查看工单明细。</p>
      </div>
      <div class="toolbar-actions">
        <el-select v-model="selectedMonth" style="width: 160px">
          <el-option v-for="month in monthOptions()" :key="month" :label="month" :value="month" />
        </el-select>
        <el-select v-if="canSelectMember" v-model="selectedMember" clearable placeholder="选择成员" style="width: 180px">
          <el-option
            v-for="name in metaStore.config?.userNames || []"
            :key="name"
            :label="name"
            :value="name"
          />
        </el-select>
        <el-button v-if="canExport" :loading="exporting" @click="exportExcel">导出 Excel</el-button>
      </div>
    </section>

    <section class="glass-card member-banner">
      <span class="section-label">当前查看成员</span>
      <strong>{{ currentMemberName }}</strong>
    </section>

    <section class="data-grid finance-grid">
      <article v-for="card in monthlyCards" :key="card.label" class="glass-card stat-box">
        <p class="section-label">{{ card.label }}</p>
        <h3>{{ card.value }}</h3>
        <p class="muted">{{ card.note }}</p>
      </article>
    </section>

    <section class="glass-card">
      <div class="detail-header">
        <div>
          <p class="section-label">Work Order Details</p>
          <h3>工单明细</h3>
        </div>
        <span class="pill">{{ summary.month }}</span>
      </div>

      <el-table :data="summary.workOrderDetails" empty-text="该月份暂无工单记录">
        <el-table-column prop="title" label="工单标题" min-width="220" />
        <el-table-column prop="dates" label="参与日期" min-width="220" />
        <el-table-column prop="hours" label="工时" width="120">
          <template #default="{ row }">{{ Number(row.hours).toFixed(1) }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="工单酬劳" width="150">
          <template #default="{ row }">{{ formatCurrency(row.amount) }}</template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>

<style scoped>
.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.finance-grid {
  align-items: stretch;
}

.glass-card {
  padding: 24px;
}

.member-banner {
  display: flex;
  gap: 10px;
  align-items: center;
}

.stat-box h3 {
  margin: 8px 0;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  margin-bottom: 18px;
  flex-wrap: wrap;
}
</style>
