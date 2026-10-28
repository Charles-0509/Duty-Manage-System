<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, h, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { fetchWorkOrders } from '@/api/services'
import { checkMonthlyReport, createMonthlyReport, deleteMonthlyDraft, downloadMonthlyFile, fetchMonthlyReport, fetchMonthlyReports, finalizeMonthlyReport, generateMonthlyReport, newMonthlyVersion, previewMonthlyReport } from '@/api/monthly'
import { useMetaStore } from '@/stores/meta'
import { downloadBlob } from '@/utils/schedule'
import type { WorkOrder } from '@/types'
import type { MonthlyConditions, MonthlyRow, MonthlySummary, MonthlyView } from '@/types/monthly'

const meta = useMetaStore()
const loading = ref(false)
const busy = ref(false)
const reports = ref<MonthlySummary[]>([])
const detail = ref<MonthlyView>()
const preview = ref<MonthlyView>()
const orders = ref<WorkOrder[]>([])
const ordersReady = ref(false)
const targetTotal = ref('')
const proxyIds = ref<number[]>([])
const amounts = reactive<Record<number, string>>({})
const selectedReportId = ref('')
const dateRange = ref<[string, string]>(defaultRange())
const conditions = reactive<MonthlyConditions>({ outputMonth: dayjs().subtract(1, 'month').format('YYYY-MM'), startDate: dateRange.value[0], endDate: dateRange.value[1], workOrderIds: [], includeManagement: true, managementMonths: 1 })
const current = computed(() => detail.value || preview.value)
const archived = computed(() => Boolean(meta.config?.semester.archived))
const latest = computed(() => Boolean(detail.value?.version.id && detail.value.report.latestVersionId === detail.value.version.id))
const canEdit = computed(() => !archived.value && (!detail.value || latest.value && detail.value.version.status === 'draft'))
const canRevise = computed(() => !archived.value && latest.value && detail.value?.version.status === 'finalized')
const canStartFromHistory = computed(() => !archived.value && Boolean(detail.value?.report.month) && (!detail.value?.version.id || latest.value && detail.value.version.status === 'legacy'))
const isLegacy = computed(() => detail.value?.version.status === 'legacy')
const canGenerate = computed(() => !archived.value && latest.value && !isLegacy.value)
const downloadKinds = computed(() => detail.value?.files.filter(kind => ['labor', 'work-study', 'records'].includes(kind)) || [])
const pendingConditions = computed(() => Boolean(detail.value && JSON.stringify(payload()) !== JSON.stringify(detail.value.conditions)))
const unsavedAllocation = computed(() => Boolean(detail.value && canEdit.value && (Number(targetTotal.value) !== Number(detail.value.targetTotal) || detail.value.rows.some(row => Number(amounts[row.memberId]) !== Number(row.declared) || proxyIds.value.includes(row.memberId) !== row.eligible))))
const editedTotal = computed(() => current.value?.rows.reduce((sum, row) => sum + Number(amounts[row.memberId] || 0), 0) || 0)
const declaredHours = computed(() => Number(current.value?.declaredTotal || 0) / 25)
const fileLabels: Record<string, string> = { labor: '劳务计算 Excel', 'work-study': '勤助转换 Excel', records: 'Word 记录表 ZIP' }

function defaultRange(month = dayjs().subtract(1, 'month').format('YYYY-MM')): [string, string] {
  const date = dayjs(`${month}-01`)
  return [date.format('YYYY-MM-DD'), date.endOf('month').format('YYYY-MM-DD')]
}

function payload(): MonthlyConditions {
  return { ...conditions, startDate: dateRange.value[0], endDate: dateRange.value[1], workOrderIds: [...conditions.workOrderIds], managementMonths: conditions.includeManagement ? conditions.managementMonths : 0 }
}

function money(value: string | number) { return `￥${Number(value || 0).toFixed(2)}` }
function sourceMoney(value: string) { return isLegacy.value ? '—' : money(value) }
function memberKey(row: MonthlyRow) { return row.memberId }
function toggleProxy(id: number, checked: boolean) { proxyIds.value = checked ? [...proxyIds.value, id] : proxyIds.value.filter(member => member !== id) }
function selectVersion(version: string) { if (detail.value) void loadReport(detail.value.report.id, version) }

async function showError(error: any) {
  let data = error?.response?.data
  if (data instanceof Blob) { try { data = JSON.parse(await data.text()) } catch { data = undefined } }
  if (data?.issues && detail.value) detail.value.issues = data.issues
  const message = data?.issues?.length ? data.issues.join('\n\n') : data?.message || error?.message || '月报操作失败'
  await ElMessageBox.alert(h('div', { style: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '60vh', overflowY: 'auto' } }, message), '月报操作失败', {
    type: 'error', confirmButtonText: '我知道了', showClose: false,
  })
}

function adopt(view: MonthlyView) {
  detail.value = view
  preview.value = undefined
  selectedReportId.value = view.report.id
  dateRange.value = [view.conditions.startDate || '', view.conditions.endDate || '']
  Object.assign(conditions, view.conditions)
  conditions.outputMonth = view.report.month
  targetTotal.value = view.targetTotal
  proxyIds.value = view.rows.filter(row => row.eligible).map(row => row.memberId)
  for (const row of view.rows) amounts[row.memberId] = row.declared
}

async function loadOrders(preserve = false) {
  ordersReady.value = false
  if (!dateRange.value?.[0] || !dateRange.value?.[1] || dayjs(dateRange.value[0]).isAfter(dateRange.value[1])) throw new Error('请选择完整且有效的统计期间')
  const months: string[] = []
  let cursor = dayjs(dateRange.value[0]).startOf('month').subtract(1, 'month')
  const end = dayjs(dateRange.value[1]).startOf('month').add(1, 'month')
  while (!cursor.isAfter(end)) { months.push(cursor.format('YYYY-MM')); cursor = cursor.add(1, 'month') }
  const groups = await Promise.all(months.map(month => fetchWorkOrders(month)))
  orders.value = [...new Map(groups.flat().map(order => [order.id, order])).values()]
  if (!preserve) conditions.workOrderIds = orders.value.filter(order => order.belongingMonth >= dateRange.value[0].slice(0, 7) && order.belongingMonth <= dateRange.value[1].slice(0, 7)).map(order => order.id)
  ordersReady.value = true
}

async function monthChanged() {
  if (!conditions.outputMonth) return
  dateRange.value = defaultRange(conditions.outputMonth)
  preview.value = undefined
  try { await loadOrders() } catch (error) { await showError(error) }
}

async function rangeChanged() {
  preview.value = undefined
  try { await loadOrders() } catch (error) { await showError(error) }
}

async function newReport() {
  detail.value = undefined
  preview.value = undefined
  selectedReportId.value = ''
  conditions.outputMonth = dayjs().subtract(1, 'month').format('YYYY-MM')
  conditions.includeManagement = true
  conditions.managementMonths = 1
  targetTotal.value = ''
  await monthChanged()
}

async function startFromHistory() {
  const month = detail.value?.report.month
  if (!month) return
  await newReport()
  conditions.outputMonth = month
  await monthChanged()
  await getPreview()
}

async function loadReport(id: string, versionId?: string) {
  loading.value = true
  try { adopt(await fetchMonthlyReport(id, versionId)) } catch (error) { await showError(error) } finally { loading.value = false }
}

async function getPreview() {
  busy.value = true
  try {
    if (!ordersReady.value) await loadOrders(true)
    const previous = preview.value
    const selected = new Set(proxyIds.value)
    preview.value = await previewMonthlyReport(payload())
    proxyIds.value = preview.value.rows.filter(row => !previous || selected.has(row.memberId)).map(row => row.memberId)
  } catch (error) { await showError(error) } finally { busy.value = false }
}

async function generate() {
  busy.value = true
  try {
    if (!targetTotal.value) throw new Error('请手动填写目标总额')
    if (detail.value) {
      adopt(await newMonthlyVersion(detail.value.report.id, { baseVersionId: detail.value.version.id, mode: 'allocate', targetTotal: targetTotal.value, proxyMemberIds: proxyIds.value }))
    } else {
      if (!ordersReady.value) await loadOrders(true)
      const fresh = await previewMonthlyReport(payload())
      if (!preview.value || fresh.sourceHash !== preview.value.sourceHash) {
        const previous = preview.value
        const selected = new Set(proxyIds.value)
        preview.value = fresh
        proxyIds.value = fresh.rows.filter(row => !previous || selected.has(row.memberId)).map(row => row.memberId)
        ElMessage.info('来源明细已更新，请核对名单后再次生成草稿')
        return
      }
      adopt(await createMonthlyReport({ ...payload(), targetTotal: targetTotal.value, proxyMemberIds: proxyIds.value, sourceHash: fresh.sourceHash }))
    }
    reports.value = await fetchMonthlyReports()
    ElMessage.success('已生成新草稿版本')
  } catch (error) { await showError(error) } finally { busy.value = false }
}

async function saveAmounts() {
  if (!detail.value) return
  const rows = detail.value.rows.map(row => ({ memberId: row.memberId, amount: amounts[row.memberId] || '' }))
  if (rows.some(row => !/^\d+(\.\d{1,2})?$/.test(row.amount))) { ElMessage.warning('请填写完整的有效金额'); return }
  const high = rows.some(row => Number(row.amount) > 1900)
  const sum = rows.reduce((value, row) => value + Math.round(Number(row.amount) * 100), 0) / 100
  try {
    await ElMessageBox.confirm(`申报合计 ${money(detail.value.targetTotal)} → ${money(sum)}，团队经费为 ${money(sum - Number(detail.value.originalTotal))}。${high ? '包含超过1900元的手调，个人不得超过1950元。' : ''}保存为新版本？`, '保存手动调整', { type: 'warning' })
  } catch { return }
  busy.value = true
  try { adopt(await newMonthlyVersion(detail.value.report.id, { baseVersionId: detail.value.version.id, mode: 'manual', proxyMemberIds: proxyIds.value, rows, confirmHighAmounts: high })); reports.value = await fetchMonthlyReports(); ElMessage.success('手动调整已保存为新版本') } catch (error) { await showError(error) } finally { busy.value = false }
}

async function refreshSource() {
  if (!detail.value) return
  let refreshedProxies = proxyIds.value
  try { await loadOrders(true); const fresh = await previewMonthlyReport(payload()); refreshedProxies = proxyIds.value.filter(id => fresh.rows.some(row => row.memberId === id)); await ElMessageBox.confirm(`实际应发 ${money(detail.value.originalTotal)} → ${money(fresh.originalTotal)}。采用当前来源创建新版本？`, '刷新来源', { type: 'warning' }) } catch (error) { if (error !== 'cancel' && error !== 'close') await showError(error); return }
  busy.value = true
  try { adopt(await newMonthlyVersion(detail.value.report.id, { baseVersionId: detail.value.version.id, mode: 'refresh', targetTotal: targetTotal.value, proxyMemberIds: refreshedProxies, conditions: payload() })); reports.value = await fetchMonthlyReports() } catch (error) { await showError(error) } finally { busy.value = false }
}

async function revise() {
  if (!detail.value) return
  busy.value = true
  try { adopt(await newMonthlyVersion(detail.value.report.id, { baseVersionId: detail.value.version.id, mode: 'copy' })); reports.value = await fetchMonthlyReports() } catch (error) { await showError(error) } finally { busy.value = false }
}

async function check() {
  if (!detail.value) return
  if (unsavedAllocation.value) { ElMessage.warning('请先保存手动调整或重新分配，核验只检查已保存版本'); return }
  busy.value = true
  try { adopt(await checkMonthlyReport(detail.value.report.id, detail.value.version.id)); if (!detail.value.issues.length) ElMessage.success('预检查通过') } catch (error) { await showError(error) } finally { busy.value = false }
}

async function finalize() {
  if (!detail.value) return
  if (unsavedAllocation.value || pendingConditions.value) { ElMessage.warning('有未保存的调整，请先保存金额、重新分配或刷新来源，再定稿'); return }
  const high = detail.value.rows.some(row => Number(row.declared) > 1900)
  try { await ElMessageBox.confirm(`定稿将重新生成三份报表并覆盖服务器上的当前文件。${high ? '包含1900–1950元的手调金额。' : ''}${detail.value.report.finalVersionId ? '此版本将替代当前有效定稿。' : ''}确认定稿？`, '月报定稿', { type: 'warning' }) } catch { return }
  busy.value = true
  try { adopt(await finalizeMonthlyReport(detail.value.report.id, detail.value.version.id, high)); reports.value = await fetchMonthlyReports(); ElMessage.success('定稿完成，三份报表已更新') } catch (error) { await showError(error) } finally { busy.value = false }
}

async function removeDraft() {
  if (!detail.value) return
  try { await ElMessageBox.confirm('删除当前草稿版本？已定稿和旧历史不会被删除。', '删除草稿', { type: 'warning' }) } catch { return }
  busy.value = true
  try { const id = detail.value.report.id; await deleteMonthlyDraft(id, detail.value.version.id); reports.value = await fetchMonthlyReports(); if (reports.value.some(report => report.id === id)) await loadReport(id); else await newReport() } catch (error) { await showError(error) } finally { busy.value = false }
}

async function generateFiles() {
  if (!detail.value) return
  if (unsavedAllocation.value || pendingConditions.value) { ElMessage.warning('请先保存调整或刷新来源，再生成报表'); return }
  busy.value = true
  try {
    adopt(await generateMonthlyReport(detail.value.report.id, detail.value.version.id))
    ElMessage.success('三份报表已生成，服务器当前 Word 文件已更新')
  } catch (error) { await showError(error) } finally { busy.value = false }
}

async function download(kind: string, sourceId?: string) {
  if (!detail.value) return
  if (!sourceId && canEdit.value && (unsavedAllocation.value || pendingConditions.value)) { ElMessage.warning('请先保存调整，下载使用已保存的版本'); return }
  busy.value = true
  const reportId = detail.value.report.id
  const versionId = detail.value.version.id
  try {
    if (!sourceId && !detail.value.files.includes(kind) && canGenerate.value) adopt(await generateMonthlyReport(reportId, versionId))
    const file = await downloadMonthlyFile(reportId, sourceId || versionId, kind, Boolean(sourceId))
    downloadBlob(file.blob, file.filename)
  } catch (error) { await showError(error) } finally { busy.value = false }
}

async function downloadAll() {
  if (!detail.value) return
  if (canEdit.value && (unsavedAllocation.value || pendingConditions.value)) { ElMessage.warning('请先保存调整，下载使用已保存的版本'); return }
  const reportId = detail.value.report.id
  const versionId = detail.value.version.id
  busy.value = true
  try {
    if (downloadKinds.value.length < 3 && canGenerate.value) adopt(await generateMonthlyReport(reportId, versionId))
    const files = await Promise.all(['labor', 'work-study', 'records'].map(kind => downloadMonthlyFile(reportId, versionId, kind)))
    ElMessage.info('将分别下载3个文件；若浏览器拦截，请允许本站自动下载多个文件')
    for (const file of files) downloadBlob(file.blob, file.filename)
  } catch (error) { await showError(error) } finally { busy.value = false }
}

function orderDates(order: WorkOrder) {
  const dates = order.workSessions.map(session => session.date).sort()
  return dates.length ? `${dates[0]} 至 ${dates.at(-1)}` : '无在册成员工时'
}

onMounted(async () => {
  loading.value = true
  try { await meta.ensureLoaded(); reports.value = await fetchMonthlyReports(); await loadOrders(); const report = reports.value.find(item => item.month === conditions.outputMonth); if (report) await loadReport(report.id) } catch (error) { await showError(error) } finally { loading.value = false }
})
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div><p class="section-label">Monthly Reports</p><h2 class="page-title">月度报表</h2><p class="page-subtitle">结算、分配与报表在同一页面完成，草稿无需定稿即可下载。</p></div>
      <div class="toolbar-actions"><el-select v-model="selectedReportId" placeholder="选择历史月份" @change="loadReport"><el-option v-for="report in reports" :key="report.id" :label="report.month || '未标月份的旧历史'" :value="report.id" /></el-select><el-button :disabled="archived" @click="newReport">新建月报</el-button></div>
    </section>
    <el-alert v-if="archived" title="当前学期已归档，仅可查看与下载" type="info" :closable="false" />
    <section class="panel-card settings-card">
      <div class="card-header"><h3>结算范围</h3><el-tag v-if="detail">{{ detail.version.status === 'legacy' || !detail.version.id ? '旧历史只读' : detail.version.status === 'finalized' ? '已定稿' : '草稿' }}</el-tag></div>
      <el-form label-position="top" class="conditions-grid">
        <el-form-item label="申报月份"><el-date-picker v-model="conditions.outputMonth" type="month" value-format="YYYY-MM" :disabled="Boolean(detail) || archived" @change="monthChanged" /></el-form-item>
        <el-form-item label="实际统计期间"><el-date-picker v-model="dateRange" type="daterange" value-format="YYYY-MM-DD" :disabled="!canEdit" @change="rangeChanged" /></el-form-item>
        <el-form-item label="项目管理薪酬"><div class="inline-fields"><el-switch v-model="conditions.includeManagement" :disabled="!canEdit" /><el-input-number v-model="conditions.managementMonths" :min="1" :max="12" :disabled="!canEdit || !conditions.includeManagement" /><span>个月</span></div></el-form-item>
        <el-form-item label="目标总额"><el-input v-model="targetTotal" inputmode="decimal" placeholder="请手动填写" :disabled="!canEdit" /></el-form-item>
      </el-form>
      <div v-if="canEdit" class="orders-section">
        <div class="toolbar-actions"><strong>计入工单（选中整单计入）</strong><el-button size="small" @click="loadOrders(true).catch(showError)">刷新工单</el-button><el-button size="small" @click="conditions.workOrderIds = orders.map(order => order.id)">全选</el-button><el-button size="small" @click="conditions.workOrderIds = []">清空</el-button></div>
        <el-alert v-if="!ordersReady" title="工单尚未完整加载，不能生成或刷新来源" type="warning" :closable="false" />
        <el-checkbox-group v-model="conditions.workOrderIds" class="order-list"><el-checkbox v-for="order in orders" :key="order.id" :value="order.id"><span>{{ order.title }} · {{ order.belongingMonth }} · {{ orderDates(order) }}</span></el-checkbox></el-checkbox-group>
      </div>
      <div class="toolbar-actions">
        <el-button v-if="!detail" :loading="busy" :disabled="archived" @click="getPreview">获取实际应发明细</el-button>
        <el-button v-if="canEdit" type="primary" :loading="busy" :disabled="!current" @click="generate">{{ detail ? '重新分配（使用冻结来源）' : '生成草稿' }}</el-button>
        <el-button v-if="detail && canEdit" :loading="busy" @click="refreshSource">刷新来源并创建新版本</el-button>
        <el-button v-if="canRevise" :loading="busy" @click="revise">创建新修订</el-button>
        <el-button v-if="canStartFromHistory" :loading="busy" @click="startFromHistory">以新来源创建本月草稿</el-button>
      </div>
      <el-alert v-if="canEdit && pendingConditions" title="结算条件已修改，尚未刷新来源；重新分配、手调和定稿仍使用已冻结的结算条件。请点击刷新来源后应用更改。" type="warning" :closable="false" />
      <p class="muted">申报记录按25元/小时折算</p>
    </section>
    <section v-if="current" class="data-grid">
      <article class="panel-card summary-card"><p class="section-label">实际应发合计</p><h3>{{ money(current.originalTotal) }}</h3></article>
      <article class="panel-card summary-card"><p class="section-label">申报合计</p><h3>{{ money(current.declaredTotal) }}</h3><span>{{ declaredHours.toFixed(1) }} 申报折算小时</span></article>
      <article class="panel-card summary-card"><p class="section-label">团队经费</p><h3>{{ money(current.teamFund) }}</h3></article>
      <article class="panel-card summary-card"><p class="section-label">未分配差额</p><h3>{{ money(current.unallocated) }}</h3></article>
    </section>
    <section v-if="current?.rows.length" class="panel-card table-card">
      <div class="card-header"><h3>实际应发明细（冻结来源）</h3><span>{{ current.rows.length }} 人</span></div>
      <div class="responsive-table" style="--table-min-width: 880px">
        <el-table :data="current.rows" :row-key="memberKey" stripe>
          <el-table-column prop="name" label="姓名" fixed min-width="130" />
          <el-table-column prop="dutyHours" label="实际值班小时" width="120" />
          <el-table-column label="值班酬劳" width="125"><template #default="{ row }">{{ sourceMoney(row.dutyAmount) }}</template></el-table-column>
          <el-table-column prop="workHours" label="实际工单小时" width="120" />
          <el-table-column label="工单酬劳" width="125"><template #default="{ row }">{{ sourceMoney(row.workAmount) }}</template></el-table-column>
          <el-table-column label="管理薪酬" width="125"><template #default="{ row }">{{ money(row.management) }}</template></el-table-column>
          <el-table-column label="实际应发" width="135"><template #default="{ row }">{{ money(row.original) }}</template></el-table-column>
        </el-table>
      </div>
    </section>
    <section v-if="current?.rows.length" class="panel-card table-card">
      <div class="card-header"><div><h3>个人申报与代发分配</h3><p class="muted">自动建议最多1900元，手调最多1950元；优先零薪酬成员800、1600，再使用其他成员额度。</p></div><div v-if="detail && canEdit" class="toolbar-actions"><span>编辑合计 {{ money(editedTotal) }}</span><el-button :loading="busy" @click="saveAmounts">保存手动调整</el-button></div></div>
      <div class="responsive-table" style="--table-min-width: 1040px">
        <el-table :data="current.rows" stripe>
          <el-table-column prop="name" label="姓名" fixed min-width="130" />
          <el-table-column label="参与代发" width="95"><template #default="{ row }"><span v-if="isLegacy">—</span><el-checkbox v-else :model-value="proxyIds.includes(row.memberId)" :disabled="!canEdit" @change="(checked: string | number | boolean) => toggleProxy(row.memberId, Boolean(checked))" /></template></el-table-column>
          <el-table-column label="个人申报额" width="165"><template #default="{ row }"><el-input v-if="detail && canEdit" v-model="amounts[row.memberId]" inputmode="decimal" /><span v-else>{{ money(row.declared) }}</span></template></el-table-column>
          <el-table-column label="需由他人代发" width="145"><template #default="{ row }">{{ sourceMoney(row.salaryOutgoing) }}</template></el-table-column>
          <el-table-column label="替成员代发" width="140"><template #default="{ row }">{{ sourceMoney(row.salaryIncoming) }}</template></el-table-column>
          <el-table-column label="替团队代发" width="140"><template #default="{ row }">{{ sourceMoney(row.teamFund) }}</template></el-table-column>
          <el-table-column label="尚未安排代发" width="145"><template #default="{ row }">{{ sourceMoney(row.salaryOutstanding) }}</template></el-table-column>
        </el-table>
      </div>
      <el-collapse v-if="current.transfers.length" class="transfer-details"><el-collapse-item title="查看逐笔代发去向"><el-table :data="current.transfers"><el-table-column prop="sourceName" label="应得成员／经费来源" /><el-table-column prop="receiverName" label="代发成员" /><el-table-column label="金额"><template #default="{ row }">{{ money(row.amount) }}</template></el-table-column></el-table></el-collapse-item></el-collapse>
    </section>
    <section v-if="detail" class="panel-card table-card">
      <div class="card-header"><h3>报表与版本</h3><el-select :model-value="detail.version.id" placeholder="仅保留财务原件" @change="selectVersion"><el-option v-for="version in detail.versions" :key="version.id" :label="`v${version.number} · ${version.status === 'legacy' ? '旧历史' : version.status === 'finalized' ? '定稿' : '草稿'} · ${version.createdAt}`" :value="version.id" /></el-select></div>
      <el-alert v-if="detail.version.legacyNote" :title="detail.version.legacyNote" type="info" :closable="false" />
      <el-alert v-for="issue in detail.issues" :key="issue" :title="issue" type="warning" :closable="false" />
      <el-alert v-for="note in detail.notes" :key="note" :title="note" type="info" :closable="false" />
      <div v-if="canEdit && detail.version.id" class="toolbar-actions"><el-button :loading="busy" @click="check">预检查</el-button><el-button type="primary" :loading="busy" @click="finalize">定稿并冻结报表</el-button><el-button type="danger" plain :loading="busy" @click="removeDraft">删除草稿</el-button></div>
      <div v-if="detail.version.id" class="toolbar-actions">
        <el-button v-if="canGenerate" :loading="busy" @click="generateFiles">生成/更新报表</el-button>
        <el-button v-if="canGenerate || downloadKinds.length === 3" type="primary" :loading="busy" @click="downloadAll">下载全部报表（3个文件）</el-button>
        <el-button v-for="kind in downloadKinds" :key="kind" :loading="busy" @click="download(kind)">{{ fileLabels[kind] }}</el-button>
      </div>
      <div v-if="detail.sources?.some(source => source.legacy && (source.hasExcel || source.hasCsv))" class="legacy-sources"><h4>历史财务原件</h4><div v-for="source in detail.sources.filter(item => item.legacy && (item.hasExcel || item.hasCsv))" :key="source.id" class="source-row"><span>{{ source.startDate }} 至 {{ source.endDate }} · {{ source.createdAt }}</span><div><el-button v-if="source.hasExcel" text @click="download('finance', source.id)">原Excel</el-button><el-button v-if="source.hasCsv" text @click="download('finance-csv', source.id)">原CSV</el-button></div></div></div>
    </section>
  </div>
</template>

<style scoped>
.settings-card,.table-card,.summary-card{padding:24px;min-width:0;max-width:100%}
.card-header{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap;margin-bottom:18px}
.card-header h3{margin:0}
.conditions-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px}
.conditions-grid :deep(.el-date-editor){width:100%;min-width:0}
.inline-fields,.toolbar-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.orders-section{border-top:1px solid var(--line);padding-top:18px;margin-bottom:18px}
.order-list{display:flex;flex-direction:column;gap:8px;margin:12px 0;max-height:220px;overflow:auto}
.order-list :deep(.el-checkbox){height:auto;margin-right:0;white-space:normal}
.order-list :deep(.el-checkbox__label){white-space:normal;overflow-wrap:anywhere}
.summary-card h3{margin:8px 0;font-size:1.55rem}
.table-card :deep(.el-alert){margin:12px 0}
.transfer-details,.legacy-sources{margin-top:20px}
.source-row{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:10px 0;border-bottom:1px solid var(--line)}
@media(max-width:760px){.conditions-grid{grid-template-columns:minmax(0,1fr)}.settings-card,.table-card,.summary-card{padding:18px}.card-header{align-items:flex-start}.card-header :deep(.el-select){max-width:100%;width:100%}.toolbar-actions>*{max-width:100%}}
</style>
