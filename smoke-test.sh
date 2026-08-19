#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$ROOT_DIR/personnel-management"
DMS_BINARY="$ROOT_DIR/dms"

if [[ ! -x "$BINARY" || ! -x "$DMS_BINARY" ]]; then
  echo "Missing built binaries. Run ./build.sh first." >&2
  exit 1
fi
if ! command -v node >/dev/null 2>&1; then
  echo "Node.js 20+ is required." >&2
  exit 1
fi

HEAD_BEFORE_HELP="$(git -C "$ROOT_DIR" rev-parse HEAD)"
STATUS_BEFORE_HELP="$(git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all)"
HELP_OUTPUT="$("$DMS_BINARY" update --help)"
if [[ "$HELP_OUTPUT" != *"dms - 机房管理系统运维工具"* ]] ||
  [[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" != "$HEAD_BEFORE_HELP" ]] ||
  [[ "$(git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all)" != "$STATUS_BEFORE_HELP" ]]; then
  echo "dms update --help must only print help." >&2
  exit 1
fi

TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/dms-smoke.XXXXXX")"
SERVER_LOG="$TEMP_ROOT/server.log"
SERVER_PID=""

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ "$status" -ne 0 && -f "$SERVER_LOG" ]]; then
    echo
    echo "--- isolated server log ---" >&2
    tail -n 200 "$SERVER_LOG" >&2
  fi
  rm -rf -- "$TEMP_ROOT"
  exit "$status"
}
trap cleanup EXIT INT TERM

mkdir -p "$TEMP_ROOT/data/semesters" "$TEMP_ROOT/templates"

TEMPLATE_SOURCE="$ROOT_DIR/data/work-study/templates/勤工助学学生工作记录表模板.docx"
if [[ ! -f "$TEMPLATE_SOURCE" && -f "$ROOT_DIR/outputs/user-data-20260818/勤工助学学生工作记录表模板.docx" ]]; then
  TEMPLATE_SOURCE="$ROOT_DIR/outputs/user-data-20260818/勤工助学学生工作记录表模板.docx"
fi
EXPECT_TEMPLATE=0
if [[ -f "$TEMPLATE_SOURCE" ]]; then
  cp "$TEMPLATE_SOURCE" "$TEMP_ROOT/templates/勤工助学学生工作记录表模板.docx"
  EXPECT_TEMPLATE=1
fi

PORT="$(node -e 'const net=require("node:net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})')"
JWT_SECRET="$(node -e 'process.stdout.write(require("node:crypto").randomBytes(32).toString("hex"))')"
ADMIN_PASSWORD="$(node -e 'process.stdout.write(require("node:crypto").randomBytes(18).toString("base64url"))')"
USER_PASSWORD="$(node -e 'process.stdout.write(require("node:crypto").randomBytes(18).toString("base64url"))')"
NEW_USER_PASSWORD="${USER_PASSWORD}-changed"
NEW_ADMIN_PASSWORD="${ADMIN_PASSWORD}-changed"

(
  cd "$ROOT_DIR/backend"
  env \
    APP_PORT="$PORT" \
    CONTROL_DATABASE_PATH="$TEMP_ROOT/data/control.db" \
    SEMESTER_DATABASE_DIR="$TEMP_ROOT/data/semesters" \
    WORK_STUDY_TEMPLATE_DIR="$TEMP_ROOT/templates" \
    JWT_SECRET="$JWT_SECRET" \
    DEFAULT_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
    ACCESS_TOKEN_TTL=7200 \
    GIN_MODE=release \
    TZ=Asia/Shanghai \
    "$BINARY"
) >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

BASE_URL="http://127.0.0.1:$PORT" \
ADMIN_PASSWORD="$ADMIN_PASSWORD" \
NEW_ADMIN_PASSWORD="$NEW_ADMIN_PASSWORD" \
USER_PASSWORD="$USER_PASSWORD" \
NEW_USER_PASSWORD="$NEW_USER_PASSWORD" \
EXPECT_TEMPLATE="$EXPECT_TEMPLATE" \
node --input-type=module <<'NODE'
import assert from 'node:assert/strict'

const baseURL = process.env.BASE_URL
const adminPassword = process.env.ADMIN_PASSWORD
const newAdminPassword = process.env.NEW_ADMIN_PASSWORD
const userPassword = process.env.USER_PASSWORD
const newUserPassword = process.env.NEW_USER_PASSWORD
const expectTemplate = process.env.EXPECT_TEMPLATE === '1'
let adminToken = ''
let adminRefreshToken = ''
let userToken = ''
let passed = 0

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms))

async function waitForServer() {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try {
      const response = await fetch(`${baseURL}/health`)
      if (response.status === 200) return
    } catch {
      // The isolated process is still starting.
    }
    await sleep(250)
  }
  throw new Error(`server did not become healthy at ${baseURL}`)
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {})
  const token = Object.hasOwn(options, 'token') ? options.token : adminToken
  if (token) headers.set('Authorization', `Bearer ${token}`)

  let body
  if (Object.hasOwn(options, 'json')) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(options.json)
  } else if (options.form) {
    body = options.form
  }

  const response = await fetch(`${baseURL}${path}`, {
    method: options.method || 'GET',
    headers,
    body,
  })
  const bytes = Buffer.from(await response.arrayBuffer())
  const contentType = response.headers.get('content-type') || ''
  let data = bytes
  if (contentType.includes('application/json') && bytes.length > 0) {
    data = JSON.parse(bytes.toString('utf8'))
  } else if (contentType.startsWith('text/') || contentType.includes('javascript')) {
    data = bytes.toString('utf8')
  }

  const expectedStatus = options.status ?? 200
  if (response.status !== expectedStatus) {
    const detail = Buffer.isBuffer(data) ? data.toString('utf8') : JSON.stringify(data)
    throw new Error(`${options.method || 'GET'} ${path}: expected ${expectedStatus}, got ${response.status}: ${detail}`)
  }
  return { response, data, bytes }
}

async function step(name, action) {
  try {
    await action()
    passed += 1
    console.log(`PASS ${String(passed).padStart(2, '0')}  ${name}`)
  } catch (error) {
    error.message = `${name}: ${error.message}`
    throw error
  }
}

function assertXLSX(result) {
  assert.equal(result.bytes.subarray(0, 2).toString('ascii'), 'PK')
  assert.ok(result.bytes.length > 500)
}

function weekCoverage(labels) {
  let odd = 0
  let even = 0
  for (const label of labels) {
    if (label.endsWith('(单)')) odd += 1
    else if (label.endsWith('(双)')) even += 1
    else {
      odd += 1
      even += 1
    }
  }
  return { odd, even }
}

await waitForServer()

await step('health and embedded SPA are available', async () => {
  const health = await request('/health', { token: null })
  assert.equal(health.data.message, 'ok')
  assert.equal(health.response.headers.get('x-content-type-options'), 'nosniff')

  const page = await request('/', { token: null })
  assert.match(page.data, /<div id="app"><\/div>/)
  const assetPath = page.data.match(/<script[^>]+src="([^"]+)"/)?.[1]
  assert.ok(assetPath, 'SPA entry script not found')
  const asset = await request(assetPath, { token: null })
  assert.ok(asset.bytes.length > 1000)
})

await step('login, protected route and refresh rotation', async () => {
  await request('/api/auth/me', { token: null, status: 401 })
  const login = await request('/api/auth/login', {
    method: 'POST',
    token: null,
    json: { username: 'admin', password: adminPassword },
  })
  assert.equal(login.data.user.role, 'ADMIN')
  assert.ok(login.data.token)
  assert.ok(login.data.refreshToken)

  adminToken = login.data.token
  const firstRefreshToken = login.data.refreshToken
  const refreshed = await request('/api/auth/refresh', {
    method: 'POST',
    token: null,
    json: { refreshToken: firstRefreshToken },
  })
  adminToken = refreshed.data.token
  adminRefreshToken = refreshed.data.refreshToken
  await request('/api/auth/refresh', {
    method: 'POST',
    token: null,
    json: { refreshToken: firstRefreshToken },
    status: 401,
  })
  const me = await request('/api/auth/me')
  assert.equal(me.data.username, 'admin')
})

await step('global work-study template uses one current endpoint', async () => {
  const status = await request('/api/templates/global')
  assert.equal(status.data.filename, '勤工助学学生工作记录表模板.docx')
  assert.equal(status.data.exists, expectTemplate)

  if (!expectTemplate) {
    const form = new FormData()
    form.append('file', new Blob(['not-a-docx']), 'invalid.docx')
    await request('/api/templates/global', { method: 'PUT', form, status: 400 })
    return
  }

  const downloaded = await request('/api/templates/global/download')
  assert.equal(downloaded.bytes.subarray(0, 2).toString('ascii'), 'PK')
  const form = new FormData()
  form.append('file', new Blob([downloaded.bytes]), 'template.docx')
  const uploaded = await request('/api/templates/global', { method: 'PUT', form })
  assert.equal(uploaded.data.exists, true)
  await request('/api/templates/global', { method: 'DELETE' })
  assert.equal((await request('/api/templates/global')).data.exists, false)
  await request('/api/templates/global', { method: 'PUT', form })
})

const members = [
  ['owner_active', '在册负责人', '202600000001', 'OWNER'],
  ['leader_active', '在册组长', '202600000002', 'LEADER'],
  ['hr_active', '在册人事', '202600000003', 'HR'],
  ['finance_active', '在册财务', '202600000004', 'FINANCE'],
  ['user_active', '在册值班员', '202600000005', 'USER'],
  ['owner_off', '移出负责人', '202600000006', 'OWNER'],
  ['user_disabled', '停用值班员', '202600000007', 'USER'],
  ['former_member', '历史移出员', '202600000008', 'USER'],
]
let usersByName = new Map()

await step('create current-semester members and enforce role access', async () => {
  for (const [username, realName, studentNumber, role] of members) {
    await request('/api/users', {
      method: 'POST',
      json: { username, realName, studentNumber, role, initialPassword: userPassword },
      status: 201,
    })
  }
  const users = await request('/api/users')
  usersByName = new Map(users.data.items.map(user => [user.username, user]))
  assert.equal(usersByName.size, members.length + 1)

  const login = await request('/api/auth/login', {
    method: 'POST',
    token: null,
    json: { username: 'user_active', password: userPassword },
  })
  await request('/api/dashboard', { token: login.data.token, status: 403 })
  const changed = await request('/api/auth/password', {
    method: 'PUT',
    token: login.data.token,
    json: { currentPassword: userPassword, newPassword: newUserPassword },
  })
  userToken = changed.data.token
  await request('/api/users', { token: changed.data.token, status: 403 })
  await request('/api/work-orders', { token: changed.data.token, status: 403 })
})

await step('system settings use current-semester values immediately', async () => {
  const before = await request('/api/system-settings')
  assert.equal(before.data.semester.active, true)
  await request('/api/system-settings', {
    method: 'PUT',
    json: {
      firstMonday: '20260817',
      workStudyContent: '冒烟测试机房运维',
      dutyRate: 50,
      workOrderRate: 75,
      mgmtLeaderRate: 775,
      mgmtOwnerRate: 1025,
    },
  })
  const after = await request('/api/system-settings')
  assert.equal(after.data.firstMonday, '20260817')
  assert.equal(after.data.dutyRate, 50)
  const meta = await request('/api/meta/config')
  assert.equal(meta.data.firstMonday, '20260817')
})

await step('odd/even availability and partial manual schedule are preserved by auto scheduling', async () => {
  const availability = new Map([
    ['owner_active', { single: ['Mon-1', 'Mon-2'], double: ['Mon-1', 'Mon-2'] }],
    ['leader_active', { single: ['Mon-1', 'Mon-2'], double: ['Mon-1', 'Mon-2'] }],
    ['hr_active', { single: ['Mon-1'], double: ['Mon-1'] }],
    ['finance_active', { single: [], double: ['Mon-1'] }],
    ['user_active', { single: ['Mon-1'], double: [] }],
  ])
  for (const [username, payload] of availability) {
    await request(`/api/availability/users/${username}`, { method: 'PUT', json: payload })
  }

  const locked = ['在册负责人', '在册值班员(单)']
  const generated = await request('/api/schedule/auto-generate', {
    method: 'POST',
    json: { perSlot: 3, schedule: { 'Mon-1': locked } },
  })
  const labels = generated.data.schedule['Mon-1']
  assert.ok(labels.includes('在册负责人(单双)'))
  assert.ok(labels.includes(locked[1]))
  assert.deepEqual(weekCoverage(labels), { odd: 3, even: 3 })
  assert.equal(labels.length, 4)

  await request('/api/schedule', { method: 'PUT', json: { schedule: generated.data.schedule } })
  const saved = await request('/api/schedule')
  assert.deepEqual([...saved.data.schedule['Mon-1']].sort(), [...labels].sort())
  assertXLSX(await request('/api/schedule/export'))
})

let includedWorkOrder
let excludedWorkOrder

await step('final schedule and work-order CRUD persist current members', async () => {
  await request('/api/final-schedules/1', {
    method: 'PUT',
    json: {
      selectedDate: '2026-08-17',
      schedule: { 'Mon-1': ['在册值班员', '历史移出员'] },
    },
  })
  const finalSchedule = await request('/api/final-schedules/1?date=2026-08-17')
  assert.equal(finalSchedule.data.source, 'saved')
  assert.deepEqual([...finalSchedule.data.schedule['Mon-1']].sort(), ['历史移出员', '在册值班员'].sort())

  includedWorkOrder = (await request('/api/work-orders', {
    method: 'POST',
    status: 201,
    json: {
      title: '计入财务的工单',
      belongingMonth: '2026-08',
      workSessions: [
        { date: '2026-08-18', workerName: '在册值班员', duration: 2 },
        { date: '2026-08-18', workerName: '历史移出员', duration: 2 },
      ],
    },
  })).data
  includedWorkOrder = (await request(`/api/work-orders/${includedWorkOrder.id}`, {
    method: 'PUT',
    json: {
      title: '计入财务的工单（已更新）',
      belongingMonth: '2026-08',
      workSessions: includedWorkOrder.workSessions,
    },
  })).data

  excludedWorkOrder = (await request('/api/work-orders', {
    method: 'POST',
    status: 201,
    json: {
      title: '不计入财务的工单',
      belongingMonth: '2026-08',
      workSessions: [{ date: '2026-08-19', workerName: '在册人事', duration: 3 }],
    },
  })).data

  const listed = await request('/api/work-orders?month=2026-08')
  assert.equal(listed.data.items.length, 2)
  assertXLSX(await request('/api/work-orders/export?month=2026-08'))
})

await step('member removal, restoration, account disable and user ordering', async () => {
  const ownerOffID = usersByName.get('owner_off').id
  const formerID = usersByName.get('former_member').id
  const disabledID = usersByName.get('user_disabled').id

  await request(`/api/users/${ownerOffID}/membership`, { method: 'DELETE' })
  await request(`/api/users/${ownerOffID}/membership`, { method: 'PATCH' })
  await request(`/api/users/${ownerOffID}/membership`, { method: 'DELETE' })
  await request(`/api/users/${formerID}/membership`, { method: 'DELETE' })
  await request(`/api/users/${disabledID}/status`, { method: 'PATCH', json: { isActive: false } })

  const users = (await request('/api/users')).data.items
  const byUsername = new Map(users.map(user => [user.username, user]))
  assert.equal(byUsername.get('owner_off').semesterMember, false)
  assert.equal(byUsername.get('owner_off').isActive, true)
  assert.equal(byUsername.get('former_member').semesterMember, false)
  assert.equal(byUsername.get('user_disabled').isActive, false)
  assert.equal(users.at(-1).username, 'user_disabled')

  const statusRank = user => !user.isActive ? 2 : !user.semesterMember ? 1 : 0
  const roleRank = new Map([['ADMIN', 0], ['OWNER', 1], ['LEADER', 2], ['HR', 3], ['FINANCE', 4], ['USER', 5]])
  for (let index = 1; index < users.length; index += 1) {
    const previous = users[index - 1]
    const current = users[index]
    assert.ok(statusRank(previous) <= statusRank(current))
    if (statusRank(previous) === statusRank(current)) {
      assert.ok(roleRank.get(previous.role) <= roleRank.get(current.role))
    }
  }
  assert.ok(users.findIndex(user => user.username === 'owner_off') < users.findIndex(user => user.username === 'former_member'))
})

let financeBatch
let financeTargetTotal

await step('finance range filters drive summary, downloads and one-click local save', async () => {
  const params = new URLSearchParams({
    startDate: '2026-08-17',
    endDate: '2026-08-23',
    workOrderIds: includedWorkOrder.id,
    includeManagement: 'true',
    managementMonths: '1',
  })
  const summary = await request(`/api/finance?${params}`)
  assert.equal(summary.data.startDate, '2026-08-17')
  assert.equal(summary.data.endDate, '2026-08-23')
  assert.ok(summary.data.totalAmount > 0)
  financeTargetTotal = (Math.round(Number(summary.data.totalAmount) / 25) * 25).toFixed(2)

  assertXLSX(await request(`/api/finance/export?${params}`))
  const csvParams = new URLSearchParams(params)
  csvParams.set('outputMonth', '2026-08')
  const csv = await request(`/api/finance/duty-csv?${csvParams}`)
  const csvText = csv.bytes.toString('utf8')
  assert.match(csvText, /在册值班员/)
  assert.doesNotMatch(csvText, /历史移出员/)

  const saved = await request('/api/finance/save-local', {
    method: 'POST',
    json: {
      startDate: '2026-08-17',
      endDate: '2026-08-23',
      workOrderIds: [includedWorkOrder.id],
      includeManagement: true,
      managementMonths: 1,
    },
  })
  financeBatch = saved.data.batch
  assert.equal(financeBatch.outputMonth, '2026-08')
  assert.deepEqual(financeBatch.workOrderIds, [includedWorkOrder.id])
  assert.equal(financeBatch.includeManagement, true)
  assert.equal(financeBatch.managementMonths, 1)

  const batches = await request('/api/labor-convert/finance-files')
  assert.ok(batches.data.items.some(item => item.id === financeBatch.id))
  await request(`/api/work-orders/${excludedWorkOrder.id}`, { method: 'DELETE' })
})

let laborRun
let manualLaborRun

await step('labor conversion persists history and all downloads', async () => {
  laborRun = (await request('/api/labor-convert/from-finance', {
    method: 'POST',
    json: { batchId: financeBatch.id, targetTotal: financeTargetTotal },
  })).data
  assert.equal(laborRun.sourceFinanceBatchId, financeBatch.id)
  assert.ok(laborRun.rows.length > 0)

  const history = await request('/api/labor-convert/history')
  assert.ok(history.data.items.some(item => item.id === laborRun.historyId))
  const detail = await request(`/api/labor-convert/history/${laborRun.historyId}`)
  assert.equal(detail.data.historyId, laborRun.historyId)
  assertXLSX(await request(`/api/labor-convert/history/${laborRun.historyId}/download`))
  assertXLSX(await request(`/api/labor-convert/history/${laborRun.historyId}/download/work-study`))
  const csv = await request(`/api/labor-convert/history/${laborRun.historyId}/download/csv`)
  assert.ok(csv.bytes.length > 50)
  if (expectTemplate) {
    const records = await request(`/api/labor-convert/history/${laborRun.historyId}/download/records`)
    assert.equal(records.bytes.subarray(0, 2).toString('ascii'), 'PK')
  }
  const personal = await request('/api/my-records', { token: userToken })
  assert.equal(personal.data.dutyRecords.length, 1)
  assert.equal(personal.data.workHours, 2)
  assert.ok(personal.data.laborHistory.some(item => item.historyId === laborRun.historyId))
  assertXLSX(await request('/api/my-records/export', { token: userToken }))
})

await step('manual labor adjustment keeps source batches protected', async () => {
  manualLaborRun = (await request(`/api/labor-convert/history/${laborRun.historyId}/manual-adjust`, {
    method: 'POST',
    json: { rows: laborRun.rows.map(row => ({ name: row.name, adjusted: row.adjusted })) },
  })).data
  assert.equal(manualLaborRun.parentRunId, laborRun.historyId)
  assert.equal(manualLaborRun.sourceFinanceBatchId, financeBatch.id)
  assert.equal(manualLaborRun.isManualAdjust, true)

  await request(`/api/labor-convert/finance-files/${financeBatch.id}`, { method: 'DELETE', status: 409 })
  await request(`/api/labor-convert/history/${manualLaborRun.historyId}`, { method: 'DELETE' })
  await request(`/api/labor-convert/finance-files/${financeBatch.id}`, { method: 'DELETE', status: 409 })
  await request(`/api/labor-convert/history/${laborRun.historyId}`, { method: 'DELETE' })
  await request(`/api/labor-convert/history/${laborRun.historyId}`, { status: 404 })
  await request(`/api/labor-convert/finance-files/${financeBatch.id}`, { method: 'DELETE' })
  await request(`/api/labor-convert/finance-files/${financeBatch.id}`, { method: 'DELETE', status: 404 })
})

let originalSemester
let nextSemester

await step('semester creation, cloning, hot switching, export and draft deletion', async () => {
  const before = await request('/api/semesters')
  originalSemester = before.data.active
  nextSemester = (await request('/api/semesters', {
    method: 'POST',
    status: 201,
    json: { name: '冒烟测试新学期', firstMonday: '20260824', cloneFromId: originalSemester.id },
  })).data
  assert.equal(nextSemester.draft, true)

  const activated = await request(`/api/semesters/${nextSemester.id}/activate`, { method: 'POST' })
  assert.equal(activated.data.active, true)
  assert.equal(activated.response.headers.get('x-dms-semester-id'), nextSemester.id)
  assert.ok(activated.data.contextVersion > originalSemester.contextVersion)

  const clonedUsers = (await request('/api/users')).data.items
  assert.ok(clonedUsers.some(user => user.username === 'owner_active'))
  assert.equal(clonedUsers.find(user => user.username === 'former_member')?.semesterMember, false)
  const clonedSettings = await request('/api/system-settings')
  assert.equal(clonedSettings.data.workStudyContent, '冒烟测试机房运维')
  assert.equal(clonedSettings.data.dutyRate, 50)

  const draft = (await request('/api/semesters', {
    method: 'POST',
    status: 201,
    json: { name: '待删除草稿', firstMonday: '20260831', cloneFromId: nextSemester.id },
  })).data
  await request(`/api/semesters/${draft.id}`, { method: 'DELETE' })

  const exported = await request(`/api/semesters/${originalSemester.id}/export`)
  assert.equal(exported.bytes.subarray(0, 16).toString('ascii'), 'SQLite format 3\u0000')
  const form = new FormData()
  form.append('file', new Blob([exported.bytes], { type: 'application/vnd.sqlite3' }), 'duplicate.db')
  await request('/api/semesters/import', { method: 'POST', form, status: 400 })
})

await step('archived semesters stay readable and reject writes with 423', async () => {
  await request(`/api/semesters/${originalSemester.id}/activate`, { method: 'POST' })
  const archivedMeta = await request('/api/meta/config')
  assert.equal(archivedMeta.data.semester.id, originalSemester.id)
  assert.equal(archivedMeta.data.semester.archived, true)
  await request('/api/schedule', { method: 'PUT', json: { schedule: {} }, status: 423 })

  await request(`/api/semesters/${nextSemester.id}/activate`, { method: 'POST' })
  await request(`/api/semesters/${nextSemester.id}/archive`, { method: 'POST' })
  await request('/api/system-settings', {
    method: 'PUT',
    status: 423,
    json: {
      firstMonday: '20260824',
      workStudyContent: '归档写入应失败',
      dutyRate: 25,
      workOrderRate: 50,
      mgmtLeaderRate: 750,
      mgmtOwnerRate: 1000,
    },
  })
  await request(`/api/semesters/${nextSemester.id}/unarchive`, { method: 'POST' })
  await request('/api/system-settings', {
    method: 'PUT',
    json: {
      firstMonday: '20260824',
      workStudyContent: '解除归档后可写',
      dutyRate: 25,
      workOrderRate: 50,
      mgmtLeaderRate: 750,
      mgmtOwnerRate: 1000,
    },
  })
})

await step('audit log contains login and representative writes', async () => {
  const audit = await request('/api/audit-logs?page=1&pageSize=100')
  assert.ok(audit.data.total > 0)
  assert.ok(audit.data.items.some(item => item.action.includes('登录成功')))
  assert.ok(audit.data.items.some(item => item.action.includes('/api/finance/save-local')))
  assert.ok(audit.data.items.some(item => item.action.includes('/api/schedule')))
})

await step('password rotation and logout invalidate old credentials', async () => {
  const oldAccessToken = adminToken
  const oldRefreshToken = adminRefreshToken
  const changed = await request('/api/auth/password', {
    method: 'PUT',
    json: { currentPassword: adminPassword, newPassword: newAdminPassword },
  })
  adminToken = changed.data.token
  adminRefreshToken = changed.data.refreshToken
  await request('/api/auth/me', { token: oldAccessToken, status: 401 })
  await request('/api/auth/refresh', {
    method: 'POST',
    token: null,
    json: { refreshToken: oldRefreshToken },
    status: 401,
  })
  await request('/api/auth/me')
  await request('/api/auth/logout', {
    method: 'POST',
    json: { refreshToken: adminRefreshToken },
  })
  await request('/api/auth/refresh', {
    method: 'POST',
    token: null,
    json: { refreshToken: adminRefreshToken },
    status: 401,
  })
})

console.log(`\nRESULT: ${passed} isolated smoke-test phases passed`)
NODE
