<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { downloadDutyCSV, downloadFinanceWorkbook, fetchFinanceSummary, fetchWorkOrders } from '@/api/services'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { defaultMonthOption, downloadBlob, monthOptions } from '@/utils/schedule'
import type { FinanceSummary, WorkOrder } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()

const loading = ref(false)
const exporting = ref(false)
const exportingCsv = ref(false)
const exportRangeVisible = ref(false)
const exportOrdersVisible = ref(false)
const csvRangeVisible = ref(false)
const loadingExportOrders = ref(false)
const selectedMonth = ref(defaultMonthOption())
const selectedMember = ref('')
const exportDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const csvDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const includeManagement = ref(false)
const managementMonths = ref(1)
const exportWorkOrders = ref<WorkOrder[]>([])
const selectedWorkOrderIds = ref<string[]>([])

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

const canExport = computed(() => authStore.hasRole(['ADMIN', 'OWNER', 'FINANCE']))
const canSelectMember = computed(() => authStore.hasRole(['ADMIN', 'OWNER', 'FINANCE']))
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

function openExportRangeDialog() {
  exportDateRange.value = monthDateRange(selectedMonth.value)
  includeManagement.value = false
  managementMonths.value = 1
  exportRangeVisible.value = true
}

function openCsvRangeDialog() {
  csvDateRange.value = monthDateRange(selectedMonth.value)
  csvRangeVisible.value = true
}

async function openWorkOrderDialog() {
  if (!exportDateRange.value?.[0] || !exportDateRange.value?.[1]) {
    ElMessage.warning('请选择完整的起止日期')
    return
  }
  if (dayjs(exportDateRange.value[0]).isAfter(dayjs(exportDateRange.value[1]))) {
    ElMessage.warning('起始日期不能晚于结束日期')
    return
  }

  exportRangeVisible.value = false
  exportOrdersVisible.value = true
  loadingExportOrders.value = true
  try {
    const months = recentWorkOrderMonths()
    const groups = await Promise.all(months.map((month) => fetchWorkOrders(month).catch(() => [])))
    exportWorkOrders.value = groups.flat()
    selectedWorkOrderIds.value = exportWorkOrders.value.map((order) => order.id)
  } catch {
    ElMessage.error('加载可选工单失败')
  } finally {
    loadingExportOrders.value = false
  }
}

async function exportCsv() {
  if (!csvDateRange.value?.[0] || !csvDateRange.value?.[1]) {
    ElMessage.warning('请选择完整的起止日期')
    return
  }
  if (dayjs(csvDateRange.value[0]).isAfter(dayjs(csvDateRange.value[1]))) {
    ElMessage.warning('起始日期不能晚于结束日期')
    return
  }

  exportingCsv.value = true
  try {
    const [startDate, endDate] = csvDateRange.value
    const blob = await downloadDutyCSV(startDate, endDate)
    downloadBlob(blob, `${compactDate(startDate)}-${compactDate(endDate)}-duty_by_person.csv`)
    csvRangeVisible.value = false
  } catch (error: any) {
    ElMessage.error(await exportErrorMessage(error, '导出值班 CSV 失败'))
  } finally {
    exportingCsv.value = false
  }
}

async function exportExcel() {
  if (!exportDateRange.value?.[0] || !exportDateRange.value?.[1]) {
    ElMessage.warning('请选择完整的起止日期')
    return
  }

  exporting.value = true
  try {
    const [startDate, endDate] = exportDateRange.value
    const blob = await downloadFinanceWorkbook({
      startDate,
      endDate,
      workOrderIds: selectedWorkOrderIds.value,
      includeManagement: includeManagement.value,
      managementMonths: includeManagement.value ? managementMonths.value : 0,
    })
    downloadBlob(blob, `${compactDate(startDate)}-${compactDate(endDate)}-财务统计.xlsx`)
    exportOrdersVisible.value = false
  } catch (error: any) {
    ElMessage.error(await exportErrorMessage(error))
  } finally {
    exporting.value = false
  }
}

function formatCurrency(amount: number) {
  return `￥ ${amount.toFixed(2)}`
}

function compactDate(value: string) {
  return value.replaceAll('-', '')
}

function monthDateRange(month: string): [string, string] {
  const start = dayjs(`${month}-01`)
  return [start.format('YYYY-MM-DD'), start.endOf('month').format('YYYY-MM-DD')]
}

function recentWorkOrderMonths() {
  const current = dayjs().startOf('month')
  return [-1, 0, 1].map((offset) => current.add(offset, 'month').format('YYYY-MM'))
}

async function exportErrorMessage(error: any, fallback = '导出财务统计失败') {
  const data = error?.response?.data
  if (data instanceof Blob) {
    try {
      const payload = JSON.parse(await data.text())
      return payload?.message || fallback
    } catch {
      return fallback
    }
  }

  return data?.message || fallback
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
        <el-button v-if="canExport" :loading="exporting" @click="openExportRangeDialog">导出 Excel</el-button>
        <el-button v-if="canExport" :loading="exportingCsv" @click="openCsvRangeDialog">导出 CSV</el-button>
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

    <el-dialog v-model="csvRangeVisible" title="选择 CSV 导出范围" width="460px">
      <el-form label-position="top">
        <el-form-item label="值班日期范围">
          <el-date-picker
            v-model="csvDateRange"
            type="daterange"
            start-placeholder="起始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <p class="muted export-hint">CSV 只统计所选日期范围内的值班记录，不包含工单和项目管理薪酬。</p>
      </el-form>
      <template #footer>
        <el-button @click="csvRangeVisible = false">取消</el-button>
        <el-button type="primary" :loading="exportingCsv" @click="exportCsv">导出 CSV</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="exportRangeVisible" title="选择导出范围" width="460px">
      <el-form label-position="top">
        <el-form-item label="统计日期范围">
          <el-date-picker
            v-model="exportDateRange"
            type="daterange"
            start-placeholder="起始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-checkbox v-model="includeManagement">包含每月项目管理薪酬</el-checkbox>
        <el-form-item v-if="includeManagement" label="项目管理薪酬月数" class="management-months-field">
          <el-input-number v-model="managementMonths" :min="1" :max="24" :step="1" :precision="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="exportRangeVisible = false">取消</el-button>
        <el-button type="primary" @click="openWorkOrderDialog">下一步</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="exportOrdersVisible" title="选择纳入计算的工单" width="720px">
      <div v-loading="loadingExportOrders">
        <p class="muted export-hint">仅显示最近三个月的工单：上月、本月和下月。已选工单会完整纳入统计，不受值班日期范围限制。</p>
        <el-checkbox-group v-model="selectedWorkOrderIds" class="workorder-check-list">
          <el-checkbox
            v-for="order in exportWorkOrders"
            :key="order.id"
            :label="order.id"
            class="workorder-check-item"
          >
            <span>{{ order.title }}</span>
            <span class="muted">{{ order.belongingMonth }}</span>
          </el-checkbox>
        </el-checkbox-group>
        <el-empty v-if="!loadingExportOrders && !exportWorkOrders.length" description="最近三个月暂无工单" />
      </div>
      <template #footer>
        <el-button @click="exportOrdersVisible = false">取消</el-button>
        <el-button type="primary" :loading="exporting" @click="exportExcel">导出 Excel</el-button>
      </template>
    </el-dialog>
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

.export-hint {
  margin: 0 0 14px;
}

.management-months-field {
  margin-top: 14px;
}

.workorder-check-list {
  display: grid;
  gap: 8px;
  max-height: 420px;
  overflow: auto;
}

.workorder-check-item {
  display: flex;
  align-items: center;
  margin-right: 0;
  padding: 10px 12px;
  border: 1px solid rgba(24, 48, 66, 0.08);
  border-radius: 8px;
}

.workorder-check-item :deep(.el-checkbox__label) {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
}
</style>
