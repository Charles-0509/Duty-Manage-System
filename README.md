# DMS 机房值班管理系统

DMS（Duty Manage System）用于管理机房成员、空闲时间、计划与实际排班、工单工时、财务批次、劳务转换和勤工助学记录表。Vue 前端会嵌入 Go 二进制，生产环境只运行一个 `personnel-management` 服务。

## 功能

- 全局账户、学期成员、角色、启停用和排序
- 单双周空闲时间、多份命名计划排班、发布和自动补排
- 实际值班、工单及工时记录
- 财务统计、Excel/CSV 导出和学期内文件留存
- 劳务转换、历史下载、手动调额和勤工助学记录表
- 学期创建、克隆、归档、导入、导出和热切换
- 审计日志、访问/刷新令牌轮换和密码失效控制
- 一致性备份、恢复、脱敏快照、更新和回退 CLI

## 技术栈与环境

- 前端：Vue 3、TypeScript、Vite、Pinia、Vue Router、Element Plus
- 后端：Go 1.26.6、Gin
- 数据库：SQLite（`modernc.org/sqlite`）
- 文件：Excelize、DOCX 模板
- 运行环境：Node.js 24+、npm、Linux systemd

生产构建使用当前仓库声明的 Go 1.26.6。Node 版本低于 24 时 `build.sh` 会直接退出。

## 数据边界

DMS 使用两层 SQLite 数据库：

- `data/control.db`：全局账户 UUID、用户名、密码哈希、姓名、学号、账户状态、会话版本、学期目录和当前学期指针。
- `data/semesters/<uuid>.db`：单学期成员关系、角色、排序、空闲时间、排班、工单、财务、劳务、设置和归档状态。
- `data/work-study/templates/勤工助学学生工作记录表模板.docx`：所有学期共享的唯一模板。

姓名和学号以 `control.db` 为权威来源；角色、排序和在册状态属于学期。成员移出学期是软移除，归档学期只读，切换学期无需重启。历史劳务记录保存生成时的姓名和学号快照。

每个学期可以保存多份计划排班，但同时最多发布一份。未发布排班仅供管理员预览和编辑；仪表盘与实际排班建议只读取当前发布表。单表可使用新版 DMS Excel 独立导入和导出。

全局模板必须包含 `{{学生学号}}` 和 `{{姓名}}`。模板不包含在学期数据库导出中，数据库恢复默认也不覆盖服务器上的模板。

这些内容是私有运行数据，不得提交：

- `backend/.env`
- `data/control.db`、`data/semesters/`
- `data/work-study/templates/`
- `outputs/`、备份、Excel/CSV/DOCX
- `frontend/node_modules/`
- 构建生成的二进制和嵌入式前端产物

## 快速开始

复制开发配置：

```bash
cp backend/.env.example backend/.env
```

至少设置以下值：

| 变量 | 说明 |
| --- | --- |
| `JWT_SECRET` | 必填，至少 32 字节的随机值，不得使用示例值 |
| `DEFAULT_ADMIN_PASSWORD` | 仅首次创建空数据库时设置，至少 12 个字符；初始化后删除 |
| `CONTROL_DATABASE_PATH` | 控制库路径，默认 `../data/control.db` |
| `SEMESTER_DATABASE_DIR` | 学期库目录，默认 `../data/semesters` |
| `WORK_STUDY_TEMPLATE_DIR` | 全局模板目录，默认 `../data/work-study/templates` |
| `APP_PORT` | 服务端口，默认 `3000`；部署环境可设为 `3100` |

其余备份和令牌有效期参数见 [`backend/.env.example`](backend/.env.example)。systemd 以 `backend/` 为工作目录，因此示例中的 `../data/...` 指向仓库根目录下的 `data/`。

首次构建：

```bash
./build.sh
```

输出：

- `personnel-management`：包含前端资源的 Web 服务
- `dms`：构建、诊断、备份、恢复、更新和回退工具

`build.sh` 默认限制 Go/Node 资源占用，适合低配置服务器。本地全速构建可执行：

```bash
LOW_RESOURCE_BUILD=0 ./build.sh
```

## 本地开发

新 checkout 必须先运行一次 `./build.sh`，生成后端所需的嵌入式前端目录。之后可分别启动后端和 Vite：

```bash
cd backend
go run ./cmd/server
```

```bash
cd frontend
npm ci
npm run dev
```

Vite 默认监听 `5173`，并把 `/api` 代理到 `http://localhost:3000`。若后端使用其他端口，请同步修改开发代理配置；生产环境不使用 Vite 开发服务器。

## 验证

按影响范围运行：

```bash
cd backend
go test ./...

cd ../frontend
npm ci
npm run test
npm run build

cd ../LaborCheckTool
go test ./...

cd ..
./build.sh
./smoke-test.sh
```

`smoke-test.sh` 使用隔离的临时数据库，是完整端到端入口。缺少私有 DOCX 模板时，与模板内容有关的下载断言会跳过，其余场景仍会执行。

安全检查：

```bash
cd frontend
npm audit --audit-level=high

cd ../backend
govulncheck ./...

cd ../LaborCheckTool
govulncheck ./...
```

GitHub Actions 会在 `main`、Pull Request 和每周计划任务中运行 `npm audit` 与 `govulncheck`。

## systemd 部署

生产机示例目录为 `/opt/DMS`：

```bash
cd /opt/DMS
./build.sh
DMS_SERVICE_USER=charles DMS_INSTALL_DIR=/opt/DMS \
  bash deploy/systemd/install-systemd.sh
sudo systemctl enable --now dms.service
sudo systemctl enable --now dms-backup.timer
```

服务直接运行根目录的二进制，工作目录为 `backend/`，配置来自 `backend/.env`。应用可监听 `0.0.0.0`，Cloudflare Tunnel 与局域网直连共用同一服务端口。

```bash
sudo systemctl status dms.service --no-pager
sudo journalctl -u dms.service -n 200 --no-pager
sudo systemctl list-timers dms-backup.timer --no-pager
```

## dms 运维工具

常用只读命令：

```bash
./dms version
./dms status
./dms doctor
./dms env
./dms logs -n 200
```

### 更新与回退

更新前必须保证发布提交已经推送，生产工作树没有需要保留的修改，并完成备份：

```bash
./dms backup
./dms update -branch main
```

`dms update` 会停止服务、获取远端代码、执行 `git reset --hard origin/main` 和 `git clean -fd`、重新构建并启动服务。生产目录中的未提交修改和未跟踪文件会被清除；不要在生产工作树保存私有文件或临时代码。

更新失败时先检查日志和数据完整性。确需回到上一次 `dms update` 前的提交时执行：

```bash
./dms rollback
```

回退代码不等同于恢复数据库。涉及 schema 变化时必须使用升级前的一致性快照，不要让旧代码直接读取新版数据库。

### 备份

```bash
./dms backup
```

快照使用 SQLite 在线一致性复制，包含控制库、全部学期库和全局模板，并更新 `latest/`。默认配置可将备份提交到专用 Git 仓库。

仅生成本地快照时必须同时禁用 Git 推送：

```bash
./dms backup -out /path/to/backup -no-git
```

不要在数据库仍可能写入时直接复制单个 `.db` 文件，否则可能遗漏 WAL 中的数据。

### 恢复

恢复是高风险操作，应先停止业务写入并确认快照完整：

```bash
./dms restore /path/to/snapshot
```

命令会先备份当前数据，再恢复控制库和全部学期库。默认保留服务器现有全局模板；只有明确需要覆盖模板时才使用：

```bash
./dms restore /path/to/snapshot -templates
```

非交互环境可追加 `-y`。恢复后运行 `./dms doctor`、启动服务并验证登录、学期、模板、财务与劳务下载。

### 脱敏快照

向开发环境提供生产数据结构时，创建一个不存在的新目录：

```bash
./dms sanitize -out /path/to/sanitized-snapshot
```

命令不会修改源数据库。它会一致性复制数据库，稳定假名化账户、姓名、学号和成员引用，统一重置账户密码，清空刷新令牌和审计日志，并删除整个 `finance_batches` 与 `labor_conversion_runs` 内容。随后执行数据库压缩，避免原始内容残留在空闲页。

脱敏快照不包含 `.env` 和全局 DOCX 模板。命令结束时显示临时登录密码；输出仍应作为受控数据保存，不得提交到代码仓库。

## 生产发布检查表

1. 确认目标提交和工作树状态。
2. 运行后端、前端、LaborCheckTool 测试。
3. 运行前端构建、`npm audit`、`govulncheck`。
4. 运行 `./build.sh` 和 `./smoke-test.sh`。
5. 推送发布提交并确认 `origin/main` 指向该提交。
6. 生产机执行 `./dms status`、`./dms doctor`、`./dms backup`。
7. 执行 `./dms update -branch main`。
8. 再次检查版本、服务、健康接口、备份定时器和日志。
9. 运行生产冒烟测试，并人工验证登录、表格、学期、模板、财务和劳务下载。

## Cloudflare Tunnel

Tunnel 可将 `dev.zfye.site`、`dms.zfye.site` 和 `duty.zfye.site` 转发到服务端口。应用仍监听所有地址，Tunnel 与局域网故障排查互不影响。

登录和刷新接口的边缘限流规则、控制台操作顺序与验证方法见 [`docs/cloudflare-rate-limits.md`](docs/cloudflare-rate-limits.md)。应用自身也保留登录失败限流，覆盖绕过 Cloudflare 的局域网请求。

## 目录

```text
backend/                    Go 服务、数据访问和 dms CLI
frontend/                   Vue 前端
LaborCheckTool/             独立劳务校验工具
deploy/systemd/             服务与自动备份单元
docs/                       运维说明
build.sh                    完整构建
smoke-test.sh               隔离端到端冒烟测试
```
