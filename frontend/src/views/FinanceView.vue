<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  downloadDutyCSV,
  downloadFinanceWorkbook,
  fetchFinanceSummary,
  fetchWorkOrders,
  saveFinanceExportsLocal,
} from '@/api/services'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { defaultMonthOption, downloadBlob, monthOptions } from '@/utils/schedule'
import type { FinanceSummary, WorkOrder } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()

const loading = ref(false)
const exporting = ref(false)
const exportingCsv = ref(false)
const savingLocal = ref(false)
const exportRangeVisible = ref(false)
const exportOrdersVisible = ref(false)
const csvRangeVisible = ref(false)
const csvOrdersVisible = ref(false)
const summaryRangeVisible = ref(false)
const summaryWorkOrderSelectorVisible = ref(false)
const loadingExportOrders = ref(false)
const loadingCsvOrders = ref(false)
const loadingSummaryOrders = ref(false)
const selectedMonth = ref(defaultMonthOption())
const selectedMember = ref('')
const summaryDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const draftSummaryDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const summaryIncludeManagement = ref(false)
const summaryManagementMonths = ref(1)
const draftSummaryIncludeManagement = ref(false)
const draftSummaryManagementMonths = ref(1)
const exportDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const csvDateRange = ref<[string, string]>(monthDateRange(selectedMonth.value))
const csvOutputMonth = ref(selectedMonth.value)
const includeManagement = ref(false)
const managementMonths = ref(1)
const csvIncludeManagement = ref(false)
const csvManagementMonths = ref(1)
const exportWorkOrders = ref<WorkOrder[]>([])
const selectedWorkOrderIds = ref<string[]>([])
const csvWorkOrders = ref<WorkOrder[]>([])
const selectedCsvWorkOrderIds = ref<string[]>([])
const summaryWorkOrders = ref<WorkOrder[]>([])
const selectedSummaryWorkOrderIds = ref<string[]>([])

const summary = ref<FinanceSummary>({
  month: selectedMonth.value,
  startDate: summaryDateRange.value[0],
  endDate: summaryDateRange.value[1],
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
const currentMemberName = computed(() => {
  if (canSelectMember.value && !selectedMember.value) {
    return '全部成员'
  }
  return selectedMember.value || authStore.user?.realName || ''
})
const showManagementCard = computed(() => summary.value.managementPending || summary.value.managementAmount > 0)
const currentRangeLabel = computed(() => `${summaryDateRange.value[0]} 至 ${summaryDateRange.value[1]}`)
const selectedSummaryWorkOrderCount = computed(() => selectedSummaryWorkOrderIds.value.length)

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
        ? summary.value.managementAmount > 0
          ? '已计算当前可结算月份，未来月份暂不计入'
          : '未来月份的项目管理薪酬会在到达当月后再计算'
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
  summaryDateRange.value = monthDateRange(selectedMonth.value)
  await loadSummaryWorkOrders(false)
  await loadSummary()
})

watch(selectedMember, async () => {
  await loadSummary()
})

onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadSummaryWorkOrders(false)
  await loadSummary()
})

async function loadSummary() {
  loading.value = true
  try {
    summary.value = await fetchFinanceSummary({
      realName: selectedMember.value,
      startDate: summaryDateRange.value[0],
      endDate: summaryDateRange.value[1],
      workOrderIds: selectedSummaryWorkOrderIds.value,
      includeManagement: summaryIncludeManagement.value,
      managementMonths: summaryIncludeManagement.value ? summaryManagementMonths.value : 0,
    })
  } catch {
    ElMessage.error('加载财务统计失败')
  } finally {
    loading.value = false
  }
}

function openSummaryRangeDialog() {
  draftSummaryDateRange.value = [...summaryDateRange.value] as [string, string]
  draftSummaryIncludeManagement.value = summaryIncludeManagement.value
  draftSummaryManagementMonths.value = summaryManagementMonths.value
  summaryRangeVisible.value = true
}

async function applySummaryDateRange() {
  if (!isValidDateRange(draftSummaryDateRange.value)) return
  summaryDateRange.value = [...draftSummaryDateRange.value] as [string, string]
  summaryIncludeManagement.value = draftSummaryIncludeManagement.value
  summaryManagementMonths.value = draftSummaryIncludeManagement.value ? draftSummaryManagementMonths.value : 1
  summaryRangeVisible.value = false
  await loadSummaryWorkOrders(false)
  await loadSummary()
}

async function openSummaryWorkOrderSelector() {
  if (!isValidDateRange(summaryDateRange.value)) return
  summaryWorkOrderSelectorVisible.value = true
  await loadSummaryWorkOrders(true)
}

async function applySummaryWorkOrders() {
  summaryWorkOrderSelectorVisible.value = false
  await loadSummary()
}

function selectAllSummaryWorkOrders() {
  selectedSummaryWorkOrderIds.value = summaryWorkOrders.value.map((order) => order.id)
}

function clearSummaryWorkOrders() {
  selectedSummaryWorkOrderIds.value = []
}

async function loadSummaryWorkOrders(preserveSelection = true) {
  loadingSummaryOrders.value = true
  try {
    const months = workOrderMonthsForDateRange(summaryDateRange.value)
    const groups = await Promise.all(months.map((month) => fetchWorkOrders(month).catch(() => [])))
    summaryWorkOrders.value = uniqueWorkOrders(groups.flat())
    if (preserveSelection) {
      const available = new Set(summaryWorkOrders.value.map((order) => order.id))
      selectedSummaryWorkOrderIds.value = selectedSummaryWorkOrderIds.value.filter((id) => available.has(id))
    } else {
      selectedSummaryWorkOrderIds.value = defaultSelectedWorkOrderIds(summaryWorkOrders.value, summaryDateRange.value)
    }
  } catch {
    ElMessage.error('加载可选工单失败')
  } finally {
    loadingSummaryOrders.value = false
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
  csvOutputMonth.value = selectedMonth.value
  csvIncludeManagement.value = false
  csvManagementMonths.value = 1
  csvRangeVisible.value = true
}

async function openCsvWorkOrderDialog() {
  if (!isValidDateRange(csvDateRange.value)) return

  csvRangeVisible.value = false
  csvOrdersVisible.value = true
  loadingCsvOrders.value = true
  try {
    const months = workOrderMonthsForDateRange(csvDateRange.value)
    const groups = await Promise.all(months.map((month) => fetchWorkOrders(month).catch(() => [])))
    csvWorkOrders.value = uniqueWorkOrders(groups.flat())
    selectedCsvWorkOrderIds.value = defaultSelectedCurrentMonthWorkOrderIds(csvWorkOrders.value)
  } catch {
    ElMessage.error('加载可选工单失败')
  } finally {
    loadingCsvOrders.value = false
  }
}

async function openWorkOrderDialog() {
  if (!isValidDateRange(exportDateRange.value)) return

  exportRangeVisible.value = false
  exportOrdersVisible.value = true
  loadingExportOrders.value = true
  try {
    const months = workOrderMonthsForDateRange(exportDateRange.value)
    const groups = await Promise.all(months.map((month) => fetchWorkOrders(month).catch(() => [])))
    exportWorkOrders.value = uniqueWorkOrders(groups.flat())
    selectedWorkOrderIds.value = defaultSelectedCurrentMonthWorkOrderIds(exportWorkOrders.value)
  } catch {
    ElMessage.error('加载可选工单失败')
  } finally {
    loadingExportOrders.value = false
  }
}

async function saveLocalExports() {
  if (!isValidDateRange(summaryDateRange.value)) return

  savingLocal.value = true
  try {
    const [startDate, endDate] = summaryDateRange.value
    await saveFinanceExportsLocal({
      startDate,
      endDate,
      workOrderIds: selectedSummaryWorkOrderIds.value,
      includeManagement: summaryIncludeManagement.value,
      managementMonths: summaryIncludeManagement.value ? summaryManagementMonths.value : 0,
    })
    ElMessage.success('Excel 和 CSV 已保存到当前学期数据库')
  } catch (error: any) {
    ElMessage.error(await exportErrorMessage(error, '保存 Excel 和 CSV 失败'))
  } finally {
    savingLocal.value = false
  }
}

async function exportCsv() {
  if (!isValidDateRange(csvDateRange.value)) return

  exportingCsv.value = true
  try {
    const [startDate, endDate] = csvDateRange.value
    const blob = await downloadDutyCSV({
      startDate,
      endDate,
      outputMonth: csvOutputMonth.value,
      workOrderIds: selectedCsvWorkOrderIds.value,
      includeManagement: csvIncludeManagement.value,
      managementMonths: csvIncludeManagement.value ? csvManagementMonths.value : 0,
    })
    downloadBlob(blob, `${compactDate(startDate)}-${compactDate(endDate)}-${compactDate(csvOutputMonth.value)}-duty_by_person.csv`)
    csvOrdersVisible.value = false
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

function isValidDateRange(range?: [string, string]) {
  if (!range?.[0] || !range?.[1]) {
    ElMessage.warning('请选择完整的起止日期')
    return false
  }
  if (dayjs(range[0]).isAfter(dayjs(range[1]))) {
    ElMessage.warning('起始日期不能晚于结束日期')
    return false
  }
  return true
}

function workOrderMonthsForDateRange(range: [string, string]) {
  const start = dayjs(range[0]).startOf('month').subtract(1, 'month')
  const end = dayjs(range[1]).startOf('month').add(1, 'month')
  const months: string[] = []
  let cursor = start
  while (!cursor.isAfter(end)) {
    months.push(cursor.format('YYYY-MM'))
    cursor = cursor.add(1, 'month')
  }
  return months
}

function monthsInsideDateRange(range: [string, string]) {
  const start = dayjs(range[0]).startOf('month')
  const end = dayjs(range[1]).startOf('month')
  const months = new Set<string>()
  let cursor = start
  while (!cursor.isAfter(end)) {
    months.add(cursor.format('YYYY-MM'))
    cursor = cursor.add(1, 'month')
  }
  return months
}

function defaultSelectedWorkOrderIds(orders: WorkOrder[], range: [string, string]) {
  const defaultMonths = monthsInsideDateRange(range)
  return orders.filter((order) => defaultMonths.has(order.belongingMonth)).map((order) => order.id)
}

function defaultSelectedCurrentMonthWorkOrderIds(orders: WorkOrder[]) {
  return orders.filter((order) => order.belongingMonth === selectedMonth.value).map((order) => order.id)
}

function uniqueWorkOrders(orders: WorkOrder[]) {
  const seen = new Set<string>()
  return orders.filter((order) => {
    if (seen.has(order.id)) return false
    seen.add(order.id)
    return true
  })
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
        <p class="page-subtitle">估算成员在所选日期段内的酬劳，并在页面下方查看计入的工单明细。</p>
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
        <el-button @click="openSummaryRangeDialog">选择日期段</el-button>
        <el-popover
          v-model:visible="summaryWorkOrderSelectorVisible"
          trigger="manual"
          placement="bottom-end"
          width="720"
        >
          <template #reference>
            <el-button :loading="loadingSummaryOrders" @click="openSummaryWorkOrderSelector">
              选择计入工单
            </el-button>
          </template>
          <div v-loading="loadingSummaryOrders" class="workorder-selector-panel">
            <div class="selector-toolbar">
              <div>
                <p class="section-label">Work Orders</p>
                <strong>已选 {{ selectedSummaryWorkOrderCount }} 个工单</strong>
              </div>
              <div class="toolbar-actions compact-actions">
                <el-button size="small" @click="selectAllSummaryWorkOrders">全选</el-button>
                <el-button size="small" @click="clearSummaryWorkOrders">清空</el-button>
              </div>
            </div>
            <p class="muted export-hint">可选范围：所选日期段覆盖月份的上月、本月和下月。</p>
            <el-checkbox-group v-model="selectedSummaryWorkOrderIds" class="workorder-check-list">
              <el-checkbox
                v-for="order in summaryWorkOrders"
                :key="order.id"
                :label="order.id"
                class="workorder-check-item"
              >
                <span>{{ order.title }}</span>
                <span class="muted">{{ order.belongingMonth }}</span>
              </el-checkbox>
            </el-checkbox-group>
            <el-empty v-if="!loadingSummaryOrders && !summaryWorkOrders.length" description="当前日期段附近暂无工单" />
            <div class="popover-actions">
              <el-button @click="summaryWorkOrderSelectorVisible = false">取消</el-button>
              <el-button type="primary" @click="applySummaryWorkOrders">应用</el-button>
            </div>
          </div>
        </el-popover>
        <el-button v-if="canExport" :loading="exporting" @click="openExportRangeDialog">导出 Excel</el-button>
        <el-button v-if="canExport" :loading="exportingCsv" @click="openCsvRangeDialog">导出 CSV</el-button>
        <el-button v-if="canExport" type="primary" plain :loading="savingLocal" @click="saveLocalExports">
          一键保存Excel和CSV
        </el-button>
      </div>
    </section>

    <section class="glass-card member-banner">
      <div>
        <span class="section-label">当前查看成员</span>
        <strong>{{ currentMemberName }}</strong>
      </div>
      <div>
        <span class="section-label">统计日期段</span>
        <strong>{{ currentRangeLabel }}</strong>
      </div>
      <div>
        <span class="section-label">计入工单</span>
        <strong>{{ selectedSummaryWorkOrderCount }} 个</strong>
      </div>
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
        <span class="pill">{{ currentRangeLabel }}</span>
      </div>

      <div class="responsive-table" style="--table-min-width: 710px">
      <el-table :data="summary.workOrderDetails" empty-text="当前范围暂无计入工单记录">
        <el-table-column prop="title" label="工单标题" min-width="220" />
        <el-table-column prop="dates" label="参与日期" min-width="220" />
        <el-table-column prop="hours" label="工时" width="120">
          <template #default="{ row }">{{ Number(row.hours).toFixed(1) }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="工单酬劳" width="150">
          <template #default="{ row }">{{ formatCurrency(row.amount) }}</template>
        </el-table-column>
      </el-table>
      </div>
    </section>

    <el-dialog v-model="summaryRangeVisible" title="选择统计日期段" width="460px">
      <el-form label-position="top">
        <el-form-item label="统计日期范围">
          <el-date-picker
            v-model="draftSummaryDateRange"
            type="daterange"
            start-placeholder="起始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-checkbox v-model="draftSummaryIncludeManagement">包含每月项目管理薪酬</el-checkbox>
        <el-form-item v-if="draftSummaryIncludeManagement" label="项目管理薪酬月数" class="management-months-field">
          <el-input-number v-model="draftSummaryManagementMonths" :min="1" :max="24" :step="1" :precision="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="summaryRangeVisible = false">取消</el-button>
        <el-button type="primary" @click="applySummaryDateRange">应用</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="csvRangeVisible" title="选择 CSV 导出范围" width="460px">
      <el-form label-position="top">
        <el-form-item label="统计日期范围">
          <el-date-picker
            v-model="csvDateRange"
            type="daterange"
            start-placeholder="起始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="导出月份">
          <el-select v-model="csvOutputMonth" style="width: 100%">
            <el-option v-for="month in monthOptions()" :key="month" :label="month" :value="month" />
          </el-select>
        </el-form-item>
        <el-checkbox v-model="csvIncludeManagement">包含每月项目管理薪酬</el-checkbox>
        <el-form-item v-if="csvIncludeManagement" label="项目管理薪酬月数" class="management-months-field">
          <el-input-number v-model="csvManagementMonths" :min="1" :max="24" :step="1" :precision="0" />
        </el-form-item>
        <p class="muted export-hint">CSV 会把值班、选中工单和项目管理薪酬统一折算为时间段，日期统一写入所选导出月份。</p>
      </el-form>
      <template #footer>
        <el-button @click="csvRangeVisible = false">取消</el-button>
        <el-button type="primary" @click="openCsvWorkOrderDialog">下一步</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="csvOrdersVisible" title="选择 CSV 纳入计算的工单" width="720px">
      <div v-loading="loadingCsvOrders">
        <p class="muted export-hint">仅显示所选日期段覆盖月份的上月、本月和下月。已选工单会按 25 元/小时折算成 CSV 时间段。</p>
        <el-checkbox-group v-model="selectedCsvWorkOrderIds" class="workorder-check-list">
          <el-checkbox
            v-for="order in csvWorkOrders"
            :key="order.id"
            :label="order.id"
            class="workorder-check-item"
          >
            <span>{{ order.title }}</span>
            <span class="muted">{{ order.belongingMonth }}</span>
          </el-checkbox>
        </el-checkbox-group>
        <el-empty v-if="!loadingCsvOrders && !csvWorkOrders.length" description="所选日期段附近暂无工单" />
      </div>
      <template #footer>
        <el-button @click="csvOrdersVisible = false">取消</el-button>
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
        <p class="muted export-hint">仅显示所选日期段覆盖月份的上月、本月和下月。已选工单会完整纳入统计，不受值班日期范围限制。</p>
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
        <el-empty v-if="!loadingExportOrders && !exportWorkOrders.length" description="所选日期段附近暂无工单" />
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
  gap: 24px;
  align-items: center;
  flex-wrap: wrap;
}

.member-banner > div {
  display: grid;
  gap: 4px;
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

.workorder-selector-panel {
  min-height: 180px;
}

.selector-toolbar {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  margin-bottom: 10px;
}

.compact-actions {
  gap: 8px;
}

.popover-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 16px;
}
</style>
