<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Download, FolderOpened, UploadFilled } from '@element-plus/icons-vue'
import {
  convertLabor,
  convertLaborFromFinance,
  downloadLaborConvertRecords,
  downloadLaborConvertWorkbook,
  fetchLaborFinanceFiles,
  fetchLaborConvertHistory,
  fetchLaborConvertHistoryDetail,
  saveLaborManualAdjustment,
} from '@/api/services'
import { downloadBlob } from '@/utils/schedule'
import type { LaborConvertHistoryItem, LaborConvertResult, LaborFinanceFileItem } from '@/types'

const fileInput = ref<HTMLInputElement>()
const sourceMode = ref<'upload' | 'finance'>('upload')
const selectedFile = ref<File>()
const selectedFinanceBatchId = ref('')
const targetTotal = ref('')
const seed = ref('')
const csvOutputMonth = ref(dayjs().format('YYYY-MM'))
const converting = ref(false)
const loadingHistory = ref(false)
const loadingFinanceFiles = ref(false)
const downloadingExcelId = ref('')
const downloadingRecordsId = ref('')
const draggingFile = ref(false)
const editingAdjustments = ref(false)
const savingAdjustment = ref(false)
const result = ref<LaborConvertResult>()
const history = ref<LaborConvertHistoryItem[]>([])
const financeFiles = ref<LaborFinanceFileItem[]>([])
const editableAmounts = ref<Record<string, string>>({})
const maxUploadSize = 100 * 1024

const summaryCards = computed(() => {
  if (!result.value) return []
  return [
    { label: '原始合计', value: moneyText(result.value.summary.originalTotal) },
    { label: '目标总额', value: moneyText(result.value.summary.targetTotal) },
    { label: '调整后合计', value: moneyText(result.value.summary.finalTotal) },
    { label: '团队经费', value: moneyText(result.value.summary.teamFund) },
  ]
})

const selectedFinanceBatch = computed(() => financeFiles.value.find((item) => item.id === selectedFinanceBatchId.value))

const editableTotal = computed(() => {
  return result.value?.rows.reduce((sum, row) => sum + Number(editableAmounts.value[row.name] || 0), 0) || 0
})

const editableTotalMatchesTarget = computed(() => {
  if (!result.value) return true
  return Math.abs(editableTotal.value - Number(result.value.summary.targetTotal || 0)) < 0.001
})

onMounted(async () => {
  await Promise.all([loadHistory(), loadFinanceFiles()])
})

function openFilePicker() {
  fileInput.value?.click()
}

function handleFileChange(event: Event) {
  const target = event.target as HTMLInputElement
  selectFile(target.files?.[0])
}

function handleDrop(event: DragEvent) {
  draggingFile.value = false
  selectFile(event.dataTransfer?.files?.[0])
}

function handleDragEnter() {
  draggingFile.value = true
}

function handleDragLeave(event: DragEvent) {
  const current = event.currentTarget as HTMLElement
  const related = event.relatedTarget as Node | null
  if (!related || !current.contains(related)) {
    draggingFile.value = false
  }
}

function selectFile(file?: File) {
  if (!file) return
  if (!isExcelFile(file)) {
    ElMessage.warning('请选择 .xlsx、.xls 或 .csv 文件')
    return
  }
  if (file.size > maxUploadSize) {
    ElMessage.warning('文件不能超过 100KB')
    return
  }
  selectedFile.value = file
}

function isExcelFile(file: File) {
  const name = file.name.toLowerCase()
  return name.endsWith('.xlsx') || name.endsWith('.xls') || name.endsWith('.csv')
}

async function submitConvert() {
  if (sourceMode.value === 'upload' && !selectedFile.value) {
    ElMessage.warning('请选择 Excel 文件')
    return
  }
  if (sourceMode.value === 'finance' && !selectedFinanceBatchId.value) {
    ElMessage.warning('请选择本地财务文件')
    return
  }
  if (!targetTotal.value) {
    ElMessage.warning('请输入目标总额')
    return
  }

  converting.value = true
  try {
    if (sourceMode.value === 'finance') {
      result.value = await convertLaborFromFinance({
        batchId: selectedFinanceBatchId.value,
        targetTotal: targetTotal.value,
        seed: seed.value.trim() || undefined,
      })
    } else {
      const formData = new FormData()
      formData.append('file', selectedFile.value as File)
      formData.append('targetTotal', targetTotal.value)
      formData.append('csvOutputMonth', csvOutputMonth.value)
      if (seed.value.trim()) {
        formData.append('seed', seed.value.trim())
      }
      result.value = await convertLabor(formData)
    }
    editingAdjustments.value = false
    ElMessage.success('劳务转换已生成')
    await loadHistory()
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '劳务转换失败'))
  } finally {
    converting.value = false
  }
}

async function loadFinanceFiles() {
  loadingFinanceFiles.value = true
  try {
    financeFiles.value = await fetchLaborFinanceFiles()
  } catch {
    ElMessage.error('加载本地财务文件失败')
  } finally {
    loadingFinanceFiles.value = false
  }
}

async function loadHistory() {
  loadingHistory.value = true
  try {
    history.value = await fetchLaborConvertHistory()
  } catch {
    ElMessage.error('加载历史记录失败')
  } finally {
    loadingHistory.value = false
  }
}

async function viewHistory(item: LaborConvertHistoryItem) {
  try {
    result.value = await fetchLaborConvertHistoryDetail(item.id)
    editingAdjustments.value = false
    ElMessage.success('已载入历史结果')
  } catch {
    ElMessage.error('加载历史详情失败')
  }
}

async function downloadExcel(item?: LaborConvertHistoryItem | LaborConvertResult) {
  const id = item && 'id' in item ? item.id : item?.historyId || result.value?.historyId
  const filename = item?.outputName || result.value?.outputName || 'labor-convert.xlsx'
  if (!id) return

  downloadingExcelId.value = id
  try {
    const blob = await downloadLaborConvertWorkbook(id)
    downloadBlob(blob, filename)
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '下载 Excel 失败'))
  } finally {
    downloadingExcelId.value = ''
  }
}

async function downloadRecords(item?: LaborConvertHistoryItem | LaborConvertResult) {
  const id = item && 'id' in item ? item.id : item?.historyId || result.value?.historyId
  const filename = recordZipName(item?.csvOutputMonth || result.value?.csvOutputMonth)
  const hasCsv = item && 'id' in item ? item.hasCsv : result.value?.hasCsv
  if (!id) return
  if (!hasCsv) {
    ElMessage.warning('该历史记录暂无可生成的记录表')
    return
  }

  downloadingRecordsId.value = id
  try {
    const blob = await downloadLaborConvertRecords(id)
    downloadBlob(blob, filename)
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '下载记录表失败'))
  } finally {
    downloadingRecordsId.value = ''
  }
}

function startEditAdjustments() {
  if (!result.value?.canManualAdjust) {
    ElMessage.warning('该历史记录不支持手动调额，请重新生成后再调整')
    return
  }
  editableAmounts.value = Object.fromEntries(result.value.rows.map((row) => [row.name, row.adjusted]))
  editingAdjustments.value = true
}

function cancelEditAdjustments() {
  editingAdjustments.value = false
  editableAmounts.value = {}
}

async function saveManualAdjustments() {
  if (!result.value) return
  if (!editableTotalMatchesTarget.value) {
    ElMessage.warning('调整后合计必须等于目标总额')
    return
  }

  savingAdjustment.value = true
  try {
    result.value = await saveLaborManualAdjustment(result.value.historyId, {
      rows: result.value.rows.map((row) => ({
        name: row.name,
        adjusted: editableAmounts.value[row.name],
      })),
    })
    editingAdjustments.value = false
    editableAmounts.value = {}
    ElMessage.success('手动调额已保存')
    await loadHistory()
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '保存手动调额失败'))
  } finally {
    savingAdjustment.value = false
  }
}

function editedDelta(row: { name: string; original: string }) {
  const adjusted = Number(editableAmounts.value[row.name] || 0)
  return (adjusted - Number(row.original || 0)).toFixed(2)
}

function recordZipName(outputMonth?: string) {
  const parsed = outputMonth ? dayjs(`${outputMonth}-01`) : null
  const month = parsed && parsed.isValid() ? parsed.month() + 1 : dayjs().month() + 1
  return `${month}月勤工助学记录表.zip`
}

function moneyText(value: string) {
  const amount = Number(value || 0)
  const prefix = amount < 0 ? '-¥' : '¥'
  return `${prefix}${Math.abs(amount).toFixed(2)}`
}

function deltaClass(value: string) {
  const amount = Number(value)
  if (amount > 0) return 'positive'
  if (amount < 0) return 'negative'
  return ''
}

async function apiErrorMessage(error: any, fallback: string) {
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
  <div class="page-shell">
    <section class="page-header">
      <div>
        <p class="section-label">Labor Convert</p>
        <h2 class="page-title">劳务转换</h2>
      </div>
      <div class="toolbar-actions">
        <el-button :icon="Download" :disabled="!result" :loading="downloadingExcelId === result?.historyId" @click="downloadExcel()">
          下载 Excel
        </el-button>
        <el-button :icon="Download" :disabled="!result?.hasCsv" :loading="downloadingRecordsId === result?.historyId" @click="downloadRecords()">
          下载记录表
        </el-button>
      </div>
    </section>

    <section class="glass-card convert-panel">
      <div class="source-panel">
        <el-segmented v-model="sourceMode" :options="[
          { label: '上传文件', value: 'upload' },
          { label: '本地财务文件', value: 'finance' },
        ]" />

        <div
          v-if="sourceMode === 'upload'"
          class="upload-box"
          :class="{ dragging: draggingFile }"
          role="button"
          tabindex="0"
          @click="openFilePicker"
          @keydown.enter="openFilePicker"
          @dragenter.prevent="handleDragEnter"
          @dragover.prevent="handleDragEnter"
          @dragleave.prevent="handleDragLeave"
          @drop.prevent="handleDrop"
        >
          <el-icon><UploadFilled /></el-icon>
          <strong>{{ selectedFile?.name || '选择财务统计 Excel' }}</strong>
        </div>

        <div v-else class="finance-file-picker">
          <el-icon><FolderOpened /></el-icon>
          <el-select
            v-model="selectedFinanceBatchId"
            :loading="loadingFinanceFiles"
            filterable
            placeholder="选择 data/finance 中的财务 Excel"
            style="width: 100%"
          >
            <el-option
              v-for="item in financeFiles"
              :key="item.id"
              :label="`${item.startDate} 至 ${item.endDate} / ${item.outputMonth}`"
              :value="item.id"
            />
          </el-select>
          <p v-if="selectedFinanceBatch" class="muted local-file-meta">
            {{ selectedFinanceBatch.excelFilename }} · {{ selectedFinanceBatch.relativeDir }}
          </p>
          <el-button :loading="loadingFinanceFiles" @click="loadFinanceFiles">刷新本地文件</el-button>
        </div>
      </div>
      <input ref="fileInput" class="hidden-input" type="file" accept=".xlsx,.xls,.csv" @change="handleFileChange" />

      <el-form label-position="top" class="convert-form">
        <el-form-item label="目标总额">
          <el-input v-model="targetTotal" placeholder="请输入数字" />
        </el-form-item>
        <el-form-item v-if="sourceMode === 'upload'" label="CSV月份">
          <el-date-picker v-model="csvOutputMonth" type="month" value-format="YYYY-MM" style="width: 100%" />
        </el-form-item>
        <el-form-item label="随机种子">
          <el-input v-model="seed" placeholder="可留空" />
        </el-form-item>
        <el-button type="primary" :loading="converting" @click="submitConvert">生成调整结果</el-button>
      </el-form>
    </section>

    <section v-if="result" class="data-grid">
      <article v-for="card in summaryCards" :key="card.label" class="glass-card stat-box">
        <p class="section-label">{{ card.label }}</p>
        <h3>{{ card.value }}</h3>
      </article>
    </section>

    <section v-if="result?.summary.warnings.length" class="warning-list">
      <el-alert
        v-for="warning in result.summary.warnings"
        :key="warning"
        type="warning"
        :title="warning"
        show-icon
        :closable="false"
      />
    </section>

    <section v-if="result" class="glass-card table-card">
      <div class="detail-header">
        <div>
          <p class="section-label">Adjustment Result</p>
          <h3>调整结果</h3>
        </div>
        <div class="result-actions">
          <span class="pill">{{ result.createdAt }}</span>
          <template v-if="editingAdjustments">
            <span :class="['pill', editableTotalMatchesTarget ? 'matched' : 'unmatched']">
              当前合计 {{ moneyText(editableTotal.toFixed(2)) }}
            </span>
            <el-button @click="cancelEditAdjustments">取消</el-button>
            <el-button type="primary" :loading="savingAdjustment" @click="saveManualAdjustments">保存调整</el-button>
          </template>
          <el-button v-else :disabled="!result.canManualAdjust" @click="startEditAdjustments">编辑调额</el-button>
        </div>
      </div>

      <el-table :data="result.rows" stripe empty-text="暂无调整结果">
        <el-table-column prop="name" label="姓名" min-width="120" fixed />
        <el-table-column prop="original" label="应发" width="120">
          <template #default="{ row }">{{ moneyText(row.original) }}</template>
        </el-table-column>
        <el-table-column prop="adjusted" label="调整后" width="120">
          <template #default="{ row }">
            <el-input
              v-if="editingAdjustments"
              v-model="editableAmounts[row.name]"
              size="small"
              inputmode="decimal"
            />
            <span v-else>{{ moneyText(row.adjusted) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="delta" label="差额" width="120">
          <template #default="{ row }">
            <span :class="['delta-text', deltaClass(editingAdjustments ? editedDelta(row) : row.delta)]">
              {{ moneyText(editingAdjustments ? editedDelta(row) : row.delta) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="260" show-overflow-tooltip />
      </el-table>
    </section>

    <section class="glass-card table-card">
      <div class="detail-header">
        <div>
          <p class="section-label">History</p>
          <h3>历史记录</h3>
        </div>
        <el-button :loading="loadingHistory" @click="loadHistory">刷新</el-button>
      </div>

      <el-table v-loading="loadingHistory" :data="history" stripe empty-text="暂无历史记录">
        <el-table-column prop="createdAt" label="生成时间" min-width="170" />
        <el-table-column prop="inputFilename" label="源文件" min-width="220" show-overflow-tooltip />
        <el-table-column prop="targetTotal" label="目标总额" width="130">
          <template #default="{ row }">{{ moneyText(row.targetTotal) }}</template>
        </el-table-column>
        <el-table-column prop="finalTotal" label="调整后合计" width="140">
          <template #default="{ row }">{{ moneyText(row.finalTotal) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="260" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="viewHistory(row)">查看</el-button>
            <el-button text :loading="downloadingExcelId === row.id" @click="downloadExcel(row)">下载 Excel</el-button>
            <el-button text :disabled="!row.hasCsv" :loading="downloadingRecordsId === row.id" @click="downloadRecords(row)">下载记录表</el-button>
          </template>
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

.convert-panel {
  display: grid;
  grid-template-columns: minmax(260px, 0.9fr) minmax(320px, 1.1fr);
  gap: 22px;
  padding: 24px;
  align-items: stretch;
}

.source-panel,
.finance-file-picker {
  display: grid;
  gap: 14px;
}

.upload-box {
  display: grid;
  place-items: center;
  gap: 12px;
  min-height: 190px;
  padding: 28px;
  border: 1px dashed rgba(15, 118, 110, 0.35);
  border-radius: var(--radius-lg);
  background: rgba(255, 255, 255, 0.58);
  color: var(--text);
  cursor: pointer;
  text-align: center;
  transition: 0.18s ease;
}

.upload-box.dragging {
  border-color: rgba(15, 118, 110, 0.85);
  background: rgba(217, 243, 239, 0.78);
  box-shadow: inset 0 0 0 1px rgba(15, 118, 110, 0.18);
}

.upload-box .el-icon {
  font-size: 42px;
  color: var(--primary);
}

.finance-file-picker {
  align-content: center;
  min-height: 190px;
  padding: 24px;
  border: 1px solid rgba(24, 48, 66, 0.08);
  border-radius: var(--radius-lg);
  background: rgba(255, 255, 255, 0.58);
}

.finance-file-picker .el-icon {
  justify-self: center;
  font-size: 38px;
  color: var(--primary);
}

.local-file-meta {
  margin: 0;
  text-align: center;
}

.hidden-input {
  display: none;
}

.convert-form {
  align-self: center;
}

.convert-form :deep(.el-button) {
  width: 100%;
}

.stat-box {
  padding: 22px;
}

.stat-box h3 {
  margin: 8px 0 0;
}

.warning-list {
  display: grid;
  gap: 10px;
}

.table-card {
  padding: 24px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  margin-bottom: 18px;
  flex-wrap: wrap;
}

.detail-header h3 {
  margin: 0;
}

.result-actions {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.pill.matched {
  color: var(--success);
}

.pill.unmatched {
  color: var(--danger);
}

.delta-text.positive {
  color: var(--success);
  font-weight: 700;
}

.delta-text.negative {
  color: var(--danger);
  font-weight: 700;
}

@media (max-width: 900px) {
  .convert-panel {
    grid-template-columns: 1fr;
  }
}
</style>
