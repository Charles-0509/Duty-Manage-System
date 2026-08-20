<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AvailabilityTable from '@/components/AvailabilityTable.vue'
import ScheduleTable from '@/components/ScheduleTable.vue'
import {
  autoGenerateSchedule,
  createSchedulePlan,
  deleteSchedulePlan,
  downloadSchedulePlanWorkbook,
  fetchAvailabilityOverview,
  fetchSchedulePlan,
  fetchSchedulePlans,
  importSchedulePlan,
  publishSchedulePlan,
  renameSchedulePlan,
  updateSchedulePlan,
} from '@/api/services'
import { useMetaStore } from '@/stores/meta'
import {
  buildShiftCode,
  buildShiftOptionsByCode,
  buildShiftStats,
  downloadBlob,
  normalizeScheduleLabels,
} from '@/utils/schedule'
import type { AvailabilityOverviewItem, SchedulePlanSummary, ViewMode } from '@/types'

const metaStore = useMetaStore()
const loading = ref(false)
const saving = ref(false)
const availabilityItems = ref<AvailabilityOverviewItem[]>([])
const plans = ref<SchedulePlanSummary[]>([])
const selectedPlanId = ref('')
const schedule = ref<Record<string, string[]>>({})
const dirty = ref(false)
const viewMode = ref<ViewMode>('all')
const autoDialogVisible = ref(false)
const autoGenerating = ref(false)
const autoWarnings = ref<string[]>([])
const autoForm = reactive({ perSlot: 1, preserveExisting: true })
const importInput = ref<HTMLInputElement>()

const currentPlan = computed(() => plans.value.find((plan) => plan.id === selectedPlanId.value))
const shiftStats = computed(() => buildShiftStats(schedule.value))
const shiftOptionsByCode = computed(() => buildShiftOptionsByCode(availabilityItems.value))

onMounted(loadPage)

async function loadPage() {
  loading.value = true
  try {
    await metaStore.ensureLoaded()
    const [overview, planItems] = await Promise.all([fetchAvailabilityOverview(), fetchSchedulePlans()])
    availabilityItems.value = overview
    plans.value = planItems
    if (planItems.length) await loadPlan(planItems[0].id)
    else startBlankPlan()
  } catch {
    ElMessage.error('加载管理员排班页面失败')
  } finally {
    loading.value = false
  }
}

async function loadPlan(id: string) {
  const result = await fetchSchedulePlan(id)
  selectedPlanId.value = id
  schedule.value = normalizeSchedule(result.schedule)
  dirty.value = false
  autoWarnings.value = []
}

function normalizeSchedule(value: Record<string, string[]>) {
  return Object.fromEntries(
    Object.entries(value).map(([shiftCode, labels]) => [shiftCode, normalizeScheduleLabels(labels)]),
  )
}

function startBlankPlan() {
  selectedPlanId.value = ''
  schedule.value = {}
  dirty.value = false
  autoWarnings.value = []
}

async function confirmDiscard() {
  if (!dirty.value) return true
  try {
    await ElMessageBox.confirm('当前排班有未保存的修改，是否放弃编辑？', '放弃未保存修改', {
      confirmButtonText: '放弃修改',
      cancelButtonText: '继续编辑',
      type: 'warning',
    })
    return true
  } catch {
    return false
  }
}

async function selectPlan(id: string) {
  if (id === selectedPlanId.value || !(await confirmDiscard())) return
  loading.value = true
  try {
    await loadPlan(id)
  } catch {
    ElMessage.error('加载排班表失败')
  } finally {
    loading.value = false
  }
}

async function createBlankPlan() {
  if (await confirmDiscard()) startBlankPlan()
}

async function persist() {
  const publishedNote = currentPlan.value?.isPublished
    ? '该表正在发布，保存后成员仪表盘会立即更新。'
    : '保存后默认不发布。'
  try {
    const { value } = await ElMessageBox.prompt(publishedNote, '保存排班表', {
      inputValue: currentPlan.value?.name || '',
      inputPlaceholder: '请输入排班表名称',
      confirmButtonText: '保存',
      cancelButtonText: '取消',
      inputValidator: validatePlanName,
    })
    saving.value = true
    const name = value.trim()
    const saved = selectedPlanId.value
      ? await updateSchedulePlan(selectedPlanId.value, name, schedule.value)
      : await createSchedulePlan(name, schedule.value)
    plans.value = await fetchSchedulePlans()
    await loadPlan(saved.id)
    ElMessage.success('排班表已保存')
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.message || '保存排班表失败')
  } finally {
    saving.value = false
  }
}

function validatePlanName(input: string) {
  const name = input.trim()
  if (!name) return '排班表名称不能为空'
  if (Array.from(name).length > 100) return '排班表名称不能超过 100 个字符'
  return true
}

async function renameCurrentPlan() {
  if (!currentPlan.value) return
  try {
    const { value } = await ElMessageBox.prompt('只修改名称，不保存当前表格中的未保存编辑。', '重命名排班表', {
      inputValue: currentPlan.value.name,
      confirmButtonText: '重命名',
      cancelButtonText: '取消',
      inputValidator: validatePlanName,
    })
    await renameSchedulePlan(currentPlan.value.id, value.trim())
    plans.value = await fetchSchedulePlans()
    ElMessage.success('排班表已重命名')
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.message || '重命名失败')
  }
}

async function removeCurrentPlan() {
  if (!currentPlan.value || currentPlan.value.isPublished || !(await confirmDiscard())) return
  try {
    await ElMessageBox.confirm(`确定删除“${currentPlan.value.name}”吗？此操作无法恢复。`, '删除排班表', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await deleteSchedulePlan(currentPlan.value.id)
    plans.value = await fetchSchedulePlans()
    if (plans.value.length) await loadPlan(plans.value[0].id)
    else startBlankPlan()
    ElMessage.success('排班表已删除')
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.message || '删除排班表失败')
  }
}

async function publishCurrentPlan() {
  if (!currentPlan.value) return
  if (dirty.value) {
    ElMessage.warning('请先保存当前修改，再发布排班表')
    return
  }
  try {
    await ElMessageBox.confirm(`发布“${currentPlan.value.name}”后，之前发布的排班表将自动取消发布。`, '发布排班表', {
      confirmButtonText: '发布',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await publishSchedulePlan(currentPlan.value.id)
    plans.value = await fetchSchedulePlans()
    ElMessage.success('排班表已发布')
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.message || '发布排班表失败')
  }
}

async function runAutoSchedule() {
  autoGenerating.value = true
  try {
    const result = await autoGenerateSchedule(autoForm.perSlot, autoForm.preserveExisting ? schedule.value : {})
    schedule.value = normalizeSchedule(result.schedule)
    dirty.value = true
    autoWarnings.value = result.warnings || []
    autoDialogVisible.value = false
    ElMessage.success('已生成排班建议，可继续手动调整后保存')
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '自动排班失败')
  } finally {
    autoGenerating.value = false
  }
}

async function exportExcel() {
  if (!currentPlan.value) return
  try {
    const blob = await downloadSchedulePlanWorkbook(currentPlan.value.id)
    downloadBlob(blob, `${currentPlan.value.name}.xlsx`)
  } catch {
    ElMessage.error('导出排班表失败')
  }
}

async function chooseImportFile() {
  if (await confirmDiscard()) importInput.value?.click()
}

async function importExcel(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    const { value } = await ElMessageBox.prompt('导入后将保存为未发布排班表。', '导入排班表', {
      inputValue: file.name.replace(/\.xlsx$/i, ''),
      confirmButtonText: '导入',
      cancelButtonText: '取消',
      inputValidator: validatePlanName,
    })
    loading.value = true
    const imported = await importSchedulePlan(file, value.trim())
    plans.value = await fetchSchedulePlans()
    await loadPlan(imported.id)
    ElMessage.success('排班表已导入')
  } catch (error: any) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.message || '导入排班表失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Schedule</p>
        <h2 class="page-title">管理员排班</h2>
        <p class="page-subtitle">排班表保存到当前学期，只有已发布的表会显示在成员仪表盘。</p>
      </div>
    </section>

    <section class="glass-card">
      <div>
        <p class="section-label">Availability</p>
        <h3>当前所有人空闲时间 (红=单周, 绿=双周, 蓝=单双周)</h3>
      </div>
      <AvailabilityTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :items="availabilityItems"
      />
    </section>

    <section class="glass-card stat-card">
      <div class="page-header">
        <div>
          <p class="section-label">Result</p>
          <h3>排班结果预览 (红=单周, 绿=双周, 蓝=单双周)</h3>
        </div>
        <el-select v-model="viewMode" style="width: 140px">
          <el-option label="总览" value="all" />
          <el-option label="仅单周" value="single" />
          <el-option label="仅双周" value="double" />
        </el-select>
      </div>
      <ScheduleTable
        v-if="metaStore.config"
        :weekdays-code="metaStore.config.weekdaysCode"
        :weekdays-display="metaStore.config.weekdaysDisplay"
        :time-slots="metaStore.config.timeSlots"
        :schedule="schedule"
        :mode="viewMode"
      />
    </section>

    <section class="glass-card">
      <div class="page-header editor-header">
        <div>
          <p class="section-label">Editor</p>
          <h3>手动排班</h3>
        </div>
        <div class="plan-controls">
          <el-select
            :model-value="selectedPlanId"
            placeholder="新排班表（未保存）"
            class="plan-select"
            @change="selectPlan"
          >
            <el-option v-for="plan in plans" :key="plan.id" :label="plan.name" :value="plan.id" />
          </el-select>
          <span v-if="currentPlan" class="publish-status" :class="currentPlan.isPublished ? 'published' : 'unpublished'">
            {{ currentPlan.isPublished ? '该表已发布' : '该表未发布' }}
          </span>
          <span v-else class="publish-status unpublished">新排班表尚未保存</span>
          <span v-if="dirty" class="pill">有未保存修改</span>
        </div>
      </div>

      <div class="toolbar-actions">
        <el-button @click="createBlankPlan">新建排班表</el-button>
        <el-button @click="autoDialogVisible = true">自动排班</el-button>
        <el-button type="primary" :loading="saving" @click="persist">保存排班</el-button>
        <el-button :disabled="!currentPlan || dirty || currentPlan.isPublished" @click="publishCurrentPlan">发布该排班表</el-button>
        <el-button :disabled="!currentPlan" @click="renameCurrentPlan">重命名</el-button>
        <el-button :disabled="!currentPlan" @click="exportExcel">导出 Excel</el-button>
        <el-button @click="chooseImportFile">导入 Excel</el-button>
        <el-button type="danger" plain :disabled="!currentPlan || currentPlan.isPublished" @click="removeCurrentPlan">删除</el-button>
        <input ref="importInput" class="file-input" type="file" accept=".xlsx" @change="importExcel" />
      </div>

      <el-alert
        v-if="autoWarnings.length"
        type="warning"
        show-icon
        :closable="true"
        title="自动排班提示"
        style="margin: 14px 0"
      >
        <div v-for="warning in autoWarnings" :key="warning">{{ warning }}</div>
      </el-alert>

      <div class="matrix-wrapper panel-card">
        <table class="matrix-table">
          <thead>
            <tr>
              <th>时间段</th>
              <th v-for="day in metaStore.config?.weekdaysDisplay || []" :key="day">{{ day }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(timeSlot, shiftIndex) in metaStore.config?.timeSlots || []" :key="timeSlot">
              <td>{{ timeSlot }}</td>
              <td v-for="dayCode in metaStore.config?.weekdaysCode || []" :key="`${timeSlot}-${dayCode}`">
                <el-select-v2
                  v-model="schedule[buildShiftCode(dayCode, shiftIndex)]"
                  class="editor-member-select"
                  :options="shiftOptionsByCode[buildShiftCode(dayCode, shiftIndex)] || []"
                  multiple
                  filterable
                  placeholder="选择人员"
                  style="width: 100%"
                  @change="dirty = true"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="glass-card stat-card">
      <p class="section-label">统计</p>
      <h3>排班班次统计</h3>
      <el-empty v-if="!shiftStats.length" description="暂无排班数据" />
      <div v-else class="stat-list">
        <div v-for="item in shiftStats" :key="item.name" class="stat-row">
          <span>{{ item.name }}</span>
          <strong>{{ item.value }} 班</strong>
        </div>
      </div>
    </section>

    <el-dialog v-model="autoDialogVisible" title="按空闲时间自动排班" width="440px">
      <p class="muted" style="margin-top: 0">系统会保留当前选择并补齐排班建议，优先组合单双周并均衡班次。</p>
      <el-form label-position="top">
        <el-form-item label="每班人数">
          <el-radio-group v-model="autoForm.perSlot">
            <el-radio-button :value="1">1 人</el-radio-button>
            <el-radio-button :value="2">2 人</el-radio-button>
            <el-radio-button :value="3">3 人</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="autoForm.preserveExisting">保留当前已选人员，只自动补齐空缺</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="autoDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="autoGenerating" @click="runAutoSchedule">生成排班建议</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.glass-card {
  padding: 24px;
}

.editor-header,
.plan-controls,
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.plan-controls {
  justify-content: flex-end;
}

.plan-select {
  width: min(320px, 70vw);
}

.publish-status {
  padding: 6px 10px;
  border-radius: 10px;
  font-weight: 700;
}

.publish-status.published {
  color: #15803d;
  background: #dcfce7;
}

.publish-status.unpublished {
  color: #dc2626;
  background: #fee2e2;
}

.toolbar-actions {
  margin-bottom: 16px;
}

.file-input {
  display: none;
}

.stat-card,
.stat-list {
  display: grid;
  gap: 18px;
}

.stat-list {
  gap: 10px;
}

.stat-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid var(--line);
}

@media (max-width: 900px) {
  .editor-header,
  .plan-controls {
    align-items: flex-start;
    width: 100%;
    justify-content: flex-start;
  }

  .plan-select {
    width: 100%;
  }
}

:deep(.editor-member-select .el-select__wrapper) {
  min-height: 42px;
  height: auto;
  align-items: flex-start;
  padding-top: 6px;
  padding-bottom: 6px;
}

:deep(.editor-member-select .el-select__selection) {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 6px;
}

:deep(.editor-member-select .el-select__selected-item) {
  max-width: 100%;
}

:deep(.editor-member-select .el-tag) {
  margin: 0;
  max-width: 100%;
  height: auto;
  white-space: normal;
  line-height: 1.35;
  padding-top: 4px;
  padding-bottom: 4px;
}
</style>
