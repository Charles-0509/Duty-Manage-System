<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Download, UploadFilled } from '@element-plus/icons-vue'
import {
  convertLabor,
  downloadLaborConvertWorkbook,
  fetchLaborConvertHistory,
  fetchLaborConvertHistoryDetail,
} from '@/api/services'
import { downloadBlob } from '@/utils/schedule'
import type { LaborConvertHistoryItem, LaborConvertResult } from '@/types'

const fileInput = ref<HTMLInputElement>()
const selectedFile = ref<File>()
const targetTotal = ref('')
const seed = ref('')
const converting = ref(false)
const loadingHistory = ref(false)
const downloadingId = ref('')
const draggingFile = ref(false)
const result = ref<LaborConvertResult>()
const history = ref<LaborConvertHistoryItem[]>([])
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

onMounted(async () => {
  await loadHistory()
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
  if (!selectedFile.value) {
    ElMessage.warning('请选择 Excel 文件')
    return
  }
  if (!targetTotal.value) {
    ElMessage.warning('请输入目标总额')
    return
  }

  const formData = new FormData()
  formData.append('file', selectedFile.value)
  formData.append('targetTotal', targetTotal.value)
  if (seed.value.trim()) {
    formData.append('seed', seed.value.trim())
  }

  converting.value = true
  try {
    result.value = await convertLabor(formData)
    ElMessage.success('劳务转换已生成')
    await loadHistory()
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '劳务转换失败'))
  } finally {
    converting.value = false
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
    ElMessage.success('已载入历史结果')
  } catch {
    ElMessage.error('加载历史详情失败')
  }
}

async function downloadResult(item?: LaborConvertHistoryItem | LaborConvertResult) {
  const id = item && 'id' in item ? item.id : item?.historyId || result.value?.historyId
  const filename = item?.outputName || result.value?.outputName || 'labor-convert.xlsx'
  if (!id) return

  downloadingId.value = id
  try {
    const blob = await downloadLaborConvertWorkbook(id)
    downloadBlob(blob, filename)
  } catch (error: any) {
    ElMessage.error(await apiErrorMessage(error, '下载失败'))
  } finally {
    downloadingId.value = ''
  }
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
        <el-button :icon="Download" :disabled="!result" :loading="downloadingId === result?.historyId" @click="downloadResult()">
          下载结果
        </el-button>
      </div>
    </section>

    <section class="glass-card convert-panel">
      <div
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
      <input ref="fileInput" class="hidden-input" type="file" accept=".xlsx,.xls,.csv" @change="handleFileChange" />

      <el-form label-position="top" class="convert-form">
        <el-form-item label="目标总额">
          <el-input v-model="targetTotal" placeholder="请输入数字" />
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
        <span class="pill">{{ result.createdAt }}</span>
      </div>

      <el-table :data="result.rows" stripe empty-text="暂无调整结果">
        <el-table-column prop="name" label="姓名" min-width="120" fixed />
        <el-table-column prop="original" label="应发" width="120">
          <template #default="{ row }">{{ moneyText(row.original) }}</template>
        </el-table-column>
        <el-table-column prop="adjusted" label="调整后" width="120">
          <template #default="{ row }">{{ moneyText(row.adjusted) }}</template>
        </el-table-column>
        <el-table-column prop="delta" label="差额" width="120">
          <template #default="{ row }">
            <span :class="['delta-text', deltaClass(row.delta)]">{{ moneyText(row.delta) }}</span>
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
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button text type="primary" @click="viewHistory(row)">查看</el-button>
            <el-button text :loading="downloadingId === row.id" @click="downloadResult(row)">下载</el-button>
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
