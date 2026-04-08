<script setup lang="ts">
import dayjs from 'dayjs'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createWorkOrder,
  deleteWorkOrder,
  downloadWorkOrderWorkbook,
  fetchWorkOrders,
  updateWorkOrder,
} from '@/api/services'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { downloadBlob, monthOptions, parsePastedSessions } from '@/utils/schedule'
import type { WorkOrder, WorkOrderDraft } from '@/types'

const authStore = useAuthStore()
const metaStore = useMetaStore()
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const pasteText = ref('')
const editingId = ref('')
const selectedMonth = ref(dayjs().format('YYYY-MM'))
const workOrders = ref<WorkOrder[]>([])

const draft = reactive<WorkOrderDraft>({
  title: '',
  belongingMonth: selectedMonth.value,
  workSessions: [{ date: dayjs().format('YYYY-MM-DD'), workerName: '', duration: 1 }],
})

const isAdmin = computed(() => authStore.user?.role === 'ADMIN')
const isNormalUser = computed(() => authStore.user?.role === 'USER')
const myHours = computed(() =>
  workOrders.value.reduce((sum: number, order: WorkOrder) => {
    return (
      sum +
      order.workSessions
        .filter((session) => session.workerName === authStore.user?.realName)
        .reduce((inner: number, session) => inner + session.duration, 0)
    )
  }, 0),
)
const myRows = computed(() =>
  workOrders.value
    .map((order: WorkOrder) => {
      const sessions = order.workSessions.filter((session) => session.workerName === authStore.user?.realName)
      const total = sessions.reduce((sum: number, session) => sum + session.duration, 0)
      if (!total) return null
      return {
        title: order.title,
        dates: sessions.map((session) => session.date).join(', '),
        hours: total,
        amount: total * 50,
      }
    })
    .filter(Boolean),
)

watch(selectedMonth, async () => {
  await loadOrders()
})

onMounted(async () => {
  await metaStore.ensureLoaded()
  await loadOrders()
})

async function loadOrders() {
  loading.value = true
  try {
    workOrders.value = await fetchWorkOrders(selectedMonth.value)
  } catch {
    ElMessage.error('加载工单失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = ''
  draft.title = ''
  draft.belongingMonth = selectedMonth.value
  draft.workSessions = [{ date: dayjs().format('YYYY-MM-DD'), workerName: '', duration: 1 }]
  pasteText.value = ''
  dialogVisible.value = true
}

function openEdit(order: WorkOrder) {
  editingId.value = order.id
  draft.title = order.title
  draft.belongingMonth = order.belongingMonth
  draft.workSessions = order.workSessions.map((session) => ({ ...session }))
  pasteText.value = ''
  dialogVisible.value = true
}

function addSession() {
  draft.workSessions.push({
    date: dayjs().format('YYYY-MM-DD'),
    workerName: '',
    duration: 1,
  })
}

function removeSession(index: number) {
  draft.workSessions.splice(index, 1)
  if (!draft.workSessions.length) addSession()
}

function applyPaste() {
  const parsed = parsePastedSessions(pasteText.value)
  if (!parsed.length) {
    ElMessage.warning('没有解析出有效记录')
    return
  }
  draft.workSessions = parsed
  ElMessage.success(`已导入 ${parsed.length} 条工时记录`)
}

async function submitDraft() {
  submitting.value = true
  try {
    if (editingId.value) {
      await updateWorkOrder(editingId.value, draft)
      ElMessage.success('工单已更新')
    } else {
      await createWorkOrder(draft)
      ElMessage.success('工单已创建')
    }
    dialogVisible.value = false
    await loadOrders()
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '保存工单失败')
  } finally {
    submitting.value = false
  }
}

async function removeOrder(id: string) {
  await ElMessageBox.confirm('删除后不可恢复，确认继续吗？', '删除工单', { type: 'warning' })
  await deleteWorkOrder(id)
  ElMessage.success('工单已删除')
  await loadOrders()
}

async function exportExcel() {
  try {
    const blob = await downloadWorkOrderWorkbook(selectedMonth.value)
    downloadBlob(blob, `${selectedMonth.value}-工单统计.xlsx`)
  } catch {
    ElMessage.error('导出工单失败')
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">Work Orders</p>
        <h2 class="page-title">工单与工时管理</h2>
        <p class="page-subtitle">
          {{ isNormalUser ? '查看你本月参与的工单、工时和金额。' : '管理员可维护工单，HR 可查看并导出月度工时统计。' }}
        </p>
      </div>
      <div class="toolbar-actions">
        <el-select v-model="selectedMonth" style="width: 160px">
          <el-option v-for="month in monthOptions()" :key="month" :label="month" :value="month" />
        </el-select>
        <el-button @click="exportExcel">导出月度工时</el-button>
        <el-button v-if="isAdmin" type="primary" @click="openCreate">新建工单</el-button>
      </div>
    </section>

    <template v-if="isNormalUser">
      <section class="data-grid">
        <article class="glass-card stat-box">
          <p class="section-label">My Hours</p>
          <h3>{{ myHours.toFixed(1) }} 小时</h3>
          <p class="muted">{{ selectedMonth }} 内你参与的总工时。</p>
        </article>
        <article class="glass-card stat-box">
          <p class="section-label">My Amount</p>
          <h3>¥ {{ (myHours * 50).toFixed(2) }}</h3>
          <p class="muted">按 50 元/小时估算。</p>
        </article>
      </section>

      <section class="glass-card">
        <div>
          <p class="section-label">My Records</p>
          <h3>我的工单明细</h3>
        </div>
        <el-table :data="myRows" empty-text="本月暂无工时记录">
          <el-table-column prop="title" label="工单标题" min-width="240" />
          <el-table-column prop="dates" label="参与日期" min-width="220" />
          <el-table-column prop="hours" label="工时" width="120" />
          <el-table-column prop="amount" label="金额" width="140">
            <template #default="{ row }">¥ {{ row.amount.toFixed(2) }}</template>
          </el-table-column>
        </el-table>
      </section>
    </template>

    <template v-else>
      <section class="glass-card">
        <div>
          <p class="section-label">Order List</p>
          <h3>本月所有工单</h3>
        </div>
        <el-empty v-if="!workOrders.length" description="该月暂无工单" />
        <el-collapse v-else accordion>
          <el-collapse-item v-for="order in workOrders" :key="order.id" :name="order.id">
            <template #title>
              <div class="collapse-title">
                <strong>{{ order.title }}</strong>
                <span class="muted">{{ order.createdBy }} · {{ order.createdTime }}</span>
              </div>
            </template>

            <div class="order-actions">
              <span class="pill">{{ order.belongingMonth }}</span>
              <div v-if="isAdmin" class="toolbar-actions">
                <el-button @click="openEdit(order)">编辑</el-button>
                <el-button type="danger" plain @click="removeOrder(order.id)">删除</el-button>
              </div>
            </div>

            <el-table :data="order.workSessions">
              <el-table-column prop="date" label="日期" width="160" />
              <el-table-column prop="workerName" label="参与人员" min-width="160" />
              <el-table-column prop="duration" label="工时" width="120" />
            </el-table>
          </el-collapse-item>
        </el-collapse>
      </section>
    </template>

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑工单' : '新建工单'"
      width="820px"
      top="4vh"
    >
      <el-form label-position="top">
        <div class="control-grid">
          <el-form-item label="工单标题">
            <el-input v-model="draft.title" placeholder="例如：服务器巡检、网络故障处理" />
          </el-form-item>
          <el-form-item label="所属月份">
            <el-select v-model="draft.belongingMonth">
              <el-option v-for="month in monthOptions()" :key="month" :label="month" :value="month" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item label="从飞书/表格粘贴工时数据">
          <el-input
            v-model="pasteText"
            type="textarea"
            :rows="4"
            placeholder="按制表符粘贴：负责人 / 状态 / 日期 / 工时"
          />
        </el-form-item>
        <el-button @click="applyPaste">解析粘贴数据</el-button>

        <div class="session-header">
          <h4>工时记录</h4>
          <el-button type="primary" plain @click="addSession">增加一行</el-button>
        </div>

        <div v-for="(session, index) in draft.workSessions" :key="`${session.date}-${index}`" class="session-row">
          <el-date-picker v-model="session.date" value-format="YYYY-MM-DD" type="date" style="width: 180px" />
          <el-select v-model="session.workerName" filterable allow-create default-first-option style="width: 220px">
            <el-option
              v-for="name in metaStore.config?.userNames || []"
              :key="name"
              :label="name"
              :value="name"
            />
          </el-select>
          <el-input-number v-model="session.duration" :min="0.5" :step="0.5" style="width: 140px" />
          <el-button type="danger" plain @click="removeSession(index)">删除</el-button>
        </div>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitDraft">保存工单</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.glass-card {
  padding: 24px;
}

.toolbar-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.stat-box h3 {
  margin: 8px 0;
}

.collapse-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 0;
}

.order-actions {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.session-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 20px 0 12px;
}

.session-row {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
</style>
