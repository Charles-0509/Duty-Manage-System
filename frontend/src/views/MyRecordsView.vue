<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import MetricCard from '@/components/MetricCard.vue'
import { downloadPersonalRecordsWorkbook, fetchPersonalRecords } from '@/api/services'
import { downloadBlob } from '@/utils/schedule'
import type { PersonalRecords } from '@/types'

const loading = ref(false)
const exporting = ref(false)
const records = ref<PersonalRecords>()

onMounted(loadRecords)

async function loadRecords() {
  loading.value = true
  try {
    records.value = await fetchPersonalRecords()
  } catch {
    ElMessage.error('加载个人记录失败')
  } finally {
    loading.value = false
  }
}

async function exportWorkbook() {
  exporting.value = true
  try {
    const blob = await downloadPersonalRecordsWorkbook()
    downloadBlob(blob, `${records.value?.realName || '个人'}-值班工时劳务记录.xlsx`)
  } catch {
    ElMessage.error('导出个人记录失败')
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div class="page-shell" v-loading="loading">
    <section class="page-header">
      <div>
        <p class="section-label">My Records</p>
        <h2 class="page-title">我的记录</h2>
        <p class="page-subtitle">
          查看当前学期已保存的实际值班、本人工单工时和按学号快照匹配的劳务历史。
        </p>
      </div>
      <el-button type="primary" :loading="exporting" @click="exportWorkbook">导出 Excel</el-button>
    </section>

    <section class="profile-line glass-card">
      <div>
        <p class="section-label">Profile</p>
        <h3>{{ records?.realName || '-' }}</h3>
      </div>
      <span class="pill">学号：{{ records?.studentNumber || '未设置' }}</span>
    </section>

    <section class="data-grid">
      <MetricCard label="实际值班班次" :value="records?.dutyCount || 0" accent="#0f766e" />
      <MetricCard label="工单工时合计" :value="`${records?.workHours || 0} 小时`" accent="#2563eb" />
      <MetricCard label="劳务调整后合计" :value="`${records?.laborAdjustedTotal || '0.00'} 元`" accent="#f97316" />
    </section>

    <section class="glass-card record-section">
      <div>
        <p class="section-label">Actual Duty</p>
        <h3>实际值班记录</h3>
      </div>
      <el-empty v-if="!records?.dutyRecords.length" description="当前学期暂无已保存的实际值班记录" />
      <div v-else class="responsive-table" style="--table-min-width: 620px">
        <el-table :data="records.dutyRecords">
          <el-table-column prop="date" label="日期" width="140" />
          <el-table-column prop="weekNumber" label="周次" width="100">
            <template #default="scope">第 {{ scope.row.weekNumber }} 周</template>
          </el-table-column>
          <el-table-column prop="weekday" label="星期" width="120" />
          <el-table-column prop="timeSlot" label="时间段" min-width="180" />
          <el-table-column prop="shiftCode" label="班次编码" width="120" />
        </el-table>
      </div>
    </section>

    <section class="glass-card record-section">
      <div>
        <p class="section-label">Work Hours</p>
        <h3>工单工时</h3>
      </div>
      <el-empty v-if="!records?.workRecords.length" description="当前学期暂无本人工单工时" />
      <div v-else class="responsive-table" style="--table-min-width: 580px">
        <el-table :data="records.workRecords">
          <el-table-column prop="date" label="日期" width="140" />
          <el-table-column prop="workOrderTitle" label="工单" min-width="280" />
          <el-table-column prop="duration" label="工时" width="120" />
        </el-table>
      </div>
    </section>

    <section class="glass-card record-section">
      <div>
        <p class="section-label">Labor History</p>
        <h3>劳务历史</h3>
      </div>
      <el-empty v-if="!records?.laborHistory.length" description="当前学期暂无与本人学号匹配的劳务历史" />
      <div v-else class="responsive-table" style="--table-min-width: 980px">
        <el-table :data="records.laborHistory">
          <el-table-column prop="createdAt" label="生成时间" width="180" />
          <el-table-column prop="inputFilename" label="来源文件" min-width="220" show-overflow-tooltip />
          <el-table-column prop="original" label="原金额" width="120" />
          <el-table-column prop="adjusted" label="调整后" width="120" />
          <el-table-column prop="tax" label="预估税额" width="120" />
          <el-table-column prop="net" label="税后" width="120" />
          <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
        </el-table>
      </div>
    </section>
  </div>
</template>

<style scoped>
.profile-line,
.record-section {
  padding: 24px;
}

.profile-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.profile-line h3,
.record-section h3 {
  margin: 8px 0 0;
}

.record-section {
  display: grid;
  gap: 18px;
}

@media (max-width: 640px) {
  .profile-line,
  .record-section {
    padding: 18px;
  }
}
</style>
