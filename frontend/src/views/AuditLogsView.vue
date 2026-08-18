<script setup lang="ts">
import { onMounted, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { fetchAuditLogs } from '@/api/services'
import type { AuditLogItem } from '@/types'

const state = reactive({
  loading: false,
  items: [] as AuditLogItem[],
  total: 0,
  page: 1,
  pageSize: 50,
  username: '',
})

onMounted(load)

async function load() {
  state.loading = true
  try {
    const result = await fetchAuditLogs(state.page, state.pageSize, state.username)
    state.items = result.items
    state.total = result.total
  } catch (error: any) {
    ElMessage.error(error?.response?.data?.message || '加载审计日志失败')
  } finally {
    state.loading = false
  }
}

function search() {
  state.page = 1
  load()
}

function statusTagType(status: number) {
  if (status >= 200 && status < 300) return 'success'
  if (status === 401 || status === 403) return 'danger'
  if (status === 423 || status === 429) return 'warning'
  return 'info'
}

</script>

<template>
  <div class="page-shell" data-page="audit-logs" v-loading="state.loading">
    <section class="page-header">
      <div>
        <p class="section-label">Audit</p>
        <h2 class="page-title">审计日志</h2>
        <p class="page-subtitle">记录登录事件与所有写操作（排班、工单、财务、账户等），共 {{ state.total }} 条，最多保留 2 万条。</p>
      </div>
      <div class="toolbar-actions">
        <el-input
          v-model="state.username"
          placeholder="按用户名筛选"
          clearable
          style="width: 200px"
          @keyup.enter="search"
          @clear="search"
        />
        <el-button type="primary" @click="search">查询</el-button>
      </div>
    </section>

    <div class="responsive-table audit-table-wrap" style="--table-min-width: 860px">
      <el-table :data="state.items" empty-text="暂无审计记录">
        <el-table-column prop="createdAt" label="时间" width="170" />
        <el-table-column prop="username" label="用户名" min-width="110" />
        <el-table-column prop="realName" label="姓名" min-width="110">
          <template #default="{ row }">{{ row.realName || '-' }}</template>
        </el-table-column>
        <el-table-column prop="action" label="操作" min-width="220" />
        <el-table-column label="结果" width="90">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="来源 IP" min-width="120" />
      </el-table>
    </div>

    <div class="pagination-row">
      <el-pagination
        v-model:current-page="state.page"
        :page-size="state.pageSize"
        :total="state.total"
        layout="prev, pager, next, total"
        background
        @current-change="load"
      />
    </div>
  </div>
</template>

<style scoped>
.toolbar-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
}

.audit-table-wrap {
  margin-top: 6px;
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 720px) {
  .toolbar-actions {
    width: 100%;
  }

  .toolbar-actions .el-input {
    width: 100% !important;
  }
}
</style>
