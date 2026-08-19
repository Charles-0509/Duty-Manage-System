# 机房管理系统

机房管理系统（DMS）面向机房运维团队，集中管理值班时间、计划排班、实际值班、工单、财务、劳务和用户。前端构建产物会嵌入 Go 后端，生产环境只运行一个二进制服务。

## 技术栈

- 前端：Vue 3、Vite、Pinia、Vue Router、Element Plus
- 后端：Go、Gin、SQLite
- 鉴权：JWT 访问令牌与刷新令牌
- 部署：单二进制、systemd

## 主要功能

- 登记单双周可值班时间并生成计划排班
- 在保留人工排班的基础上自动补齐剩余班次
- 维护实际值班、工单与工时
- 查询并导出本人的实际值班、工单工时和劳务历史
- 统计并导出财务 Excel、CSV
- 从财务文件生成劳务结果与勤工助学记录表
- 管理全局账户、当前学期成员、角色和学号
- 每学期独立 SQLite 数据库，支持创建、归档、导入、导出和热切换
- 所有学期共用一份勤工助学 Word 模板

## 数据模型

DMS 只使用当前两层数据库结构：

- `data/control.db`：全局账户、密码哈希、姓名、学号、账户状态、学期目录和当前学期指针。
- `data/semesters/<uuid>.db`：该学期的成员关系、角色、排班、实际值班、工单、财务文件和劳务历史。
- `data/work-study/templates/`：服务器全局模板目录，不属于任何学期数据库。

成员姓名和学号以控制库账户为权威数据；学期库保留业务发生时的快照。归档学期只读，切换学期无需重启服务。单双周起始日期、勤工助学工作内容和各类薪酬标准均在当前学期数据库中，通过“系统设置”维护。

通用模板文件为 `勤工助学学生工作记录表模板.docx`，必须包含 `{{学生学号}}` 和 `{{姓名}}` 占位符。导出学期数据库不会包含模板。

## 目录结构

```text
Duty-Manage-System/
├─ backend/
│  ├─ cmd/server/                 # Web 服务入口
│  ├─ cmd/dms/                    # 运维命令行
│  └─ internal/
├─ frontend/
├─ data/
│  ├─ control.db
│  ├─ semesters/<uuid>.db
│  └─ work-study/templates/
├─ deploy/systemd/
│  ├─ dms.service
│  ├─ dms-backup.service
│  ├─ dms-backup.timer
│  └─ install-systemd.sh
├─ build.sh
├─ smoke-test.sh
└─ README.md
```

## 运行时配置

生产配置位于 `backend/.env`。可以从示例复制后修改：

```bash
cp backend/.env.example backend/.env
```

当前运行时配置只有服务器级参数：

```env
APP_PORT=3000
CONTROL_DATABASE_PATH=../data/control.db
SEMESTER_DATABASE_DIR=../data/semesters
WORK_STUDY_TEMPLATE_DIR=../data/work-study/templates

JWT_SECRET=replace-with-a-long-random-secret
ACCESS_TOKEN_TTL=7200
# 只在首次创建空数据库时设置，初始化完成后删除
DEFAULT_ADMIN_PASSWORD=replace-with-a-strong-initial-password

BACKUP_DIR=/home/charles/DMS-backup
BACKUP_GIT_ENABLED=1
BACKUP_GIT_REPO=git@github.com:Charles-0509/DMS-backup.git
BACKUP_GIT_BRANCH=main
BACKUP_SSH_KEY=/home/charles/.ssh/dms_backup_ed25519
BACKUP_GIT_AUTHOR_NAME="DMS Backup"
BACKUP_GIT_AUTHOR_EMAIL=dms-backup@localhost
```

- `APP_PORT`：Web 服务监听端口。
- `CONTROL_DATABASE_PATH`：控制数据库路径。
- `SEMESTER_DATABASE_DIR`：学期数据库目录。
- `WORK_STUDY_TEMPLATE_DIR`：所有学期共用的 DOCX 模板目录。
- `JWT_SECRET`：JWT 签名密钥，必须显式设置为至少 32 字节的随机值且不得提交到仓库。
- `ACCESS_TOKEN_TTL`：访问令牌有效期，单位为秒；刷新令牌固定有效 7 天。
- `DEFAULT_ADMIN_PASSWORD`：仅在创建第一个数据库时使用，至少 12 个字符；初始化完成后从运行环境删除。
- `BACKUP_*`：本地快照目录和可选的 Git 备份仓库配置。

systemd 以 `backend/` 为工作目录，因此示例中的 `../data/...` 会指向项目根目录的 `data/`。

## 构建

Linux：

```bash
cd /opt/DMS
./build.sh
```

构建完成后会生成：

- `personnel-management`：包含前端资源的 Web 服务二进制。
- `dms`：更新、备份、恢复和诊断工具。

`build.sh` 默认限制 Go 和 Node 的构建并发，适用于低配置 Linux 服务器；本地需要全速构建时可设置 `LOW_RESOURCE_BUILD=0`。

## Linux systemd 部署

生产环境只通过 systemd 运行 `personnel-management`，不使用脚本代理启动。仓库中的 `dms.service` 直接执行二进制：

```ini
[Service]
WorkingDirectory=/opt/DMS/backend
EnvironmentFile=/opt/DMS/backend/.env
ExecStart=/opt/DMS/personnel-management
Restart=on-failure
```

安装并启动：

```bash
cd /opt/DMS
DMS_SERVICE_USER=charles DMS_INSTALL_DIR=/opt/DMS \
  bash deploy/systemd/install-systemd.sh
sudo systemctl enable --now dms.service
sudo systemctl status dms.service --no-pager
```

常用服务操作：

```bash
sudo systemctl restart dms.service
sudo systemctl stop dms.service
sudo journalctl -u dms.service -n 200 -f
```

## dms 运维命令行

所有生产更新和数据操作统一使用项目根目录的 `dms`：

```bash
./dms status                  # systemd 状态、健康检查和数据概况
./dms logs -n 200 -f          # 查看 systemd 日志
./dms doctor                  # 检查配置、依赖、端口、数据库和备份目录
./dms env                     # 显示脱敏后的生效配置
./dms version                 # 显示二进制和 Git 版本
```

### 更新

```bash
cd /opt/DMS
./dms update -branch main
```

`dms update` 会以远端分支为准更新代码、重新构建，并通过 systemd 重启服务；失败时会恢复服务状态。需要回到上一次更新前的版本时执行：

```bash
./dms rollback
```

### 备份

```bash
cd /opt/DMS
./dms backup
```

每个快照包含：

- 使用 SQLite 在线一致性备份生成的 `control.db` 副本；
- `semesters/` 中的全部学期数据库；
- 单独保存的 `work-study/templates/` 全局模板目录。

命令会创建时间戳快照并更新 `latest/`。可用 `-out <目录>` 指定备份位置，或用 `-no-git` 只保留本地快照。

### 恢复

默认恢复只覆盖控制库和全部学期数据库，不覆盖服务器当前模板：

```bash
./dms restore /home/charles/DMS-backup/latest
```

只有明确需要同时回退全局模板时才使用 `-templates`：

```bash
./dms restore /home/charles/DMS-backup/latest -templates
```

恢复前会自动备份当前数据；非交互执行可以追加 `-y`。学期数据库导出文件不包含 DOCX 模板，不能作为模板备份使用。

### 脱敏快照

需要把生产数据结构带到隔离开发环境时，使用新目录生成脱敏副本：

```bash
./dms sanitize -out /path/to/sanitized-snapshot
```

命令使用 SQLite 在线一致性复制，不修改源数据库。输出会稳定假名化账户、姓名、学号和业务成员引用，清空刷新令牌与审计日志，删除财务及劳务文件 BLOB，并再次压缩数据库以清除空闲页中的原始内容。全局 DOCX 模板和 `.env` 不会复制；命令结束时会显示脱敏副本的临时登录密码。

## 自动备份

`dms-backup.timer` 默认每天 `04:00` 调用 `dms backup`：

```bash
sudo systemctl enable --now dms-backup.timer
sudo systemctl status dms-backup.timer --no-pager
sudo systemctl list-timers dms-backup.timer --no-pager
```

如启用 Git 备份，先为生产用户配置专用 SSH key，并在 `backend/.env` 中填写对应的 `BACKUP_*` 参数。

## 端到端冒烟测试

项目根目录的 `smoke-test.sh` 是当前唯一的端到端冒烟入口。每次合并或生产部署前执行：

```bash
cd /opt/DMS
./smoke-test.sh
```

脚本返回成功后，才继续生产更新或发布。

## 安全检查

`.github/workflows/security.yml` 在 main、Pull Request 和每周计划任务中运行 `npm audit --audit-level=high` 及 `govulncheck ./...`。Cloudflare Tunnel 的登录与刷新接口边缘限流规则见 [`docs/cloudflare-rate-limits.md`](docs/cloudflare-rate-limits.md)。应用仍监听配置端口的所有地址，局域网直连由应用自身登录限流保护。

## 开发模式

前端：

```bash
cd frontend
npm install
npm run dev
```

后端：

```bash
cd backend
go run ./cmd/server
```

开发服务会读取 `backend/.env`。生产环境始终使用构建后的二进制和 systemd。
