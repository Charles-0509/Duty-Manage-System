# ✨机房管理系统

机房管理系统是一个面向机房运维团队的排班、工单、财务统计和用户管理平台。项目采用前后端一体化部署方式：前端构建后嵌入 Go 后端，最终只运行一个服务进程。

## 技术栈

- 前端：Vue 3 + Vite + Pinia + Vue Router + Element Plus
- 后端：Go + Gin + SQLite
- 鉴权：JWT

## 主要功能

- 值班人员登记单双周可值班时间
- 管理员、人事专员、负责人维护排班和实际值班
- 工单管理、工时记录、Excel 导出
- 财务统计与 Excel 导出
- 用户角色、账户状态、密码管理
- 每学期独立 SQLite 数据库、归档和无重启热切换
- 财务与劳务文件随学期数据库保存
- 所有学期共用的勤工助学 Word 模板管理

## 目录结构

```text
Duty-Manage-System/
├─ backend/
│  ├─ cmd/server/
│  ├─ internal/
│  │  ├─ config/
│  │  ├─ http/
│  │  ├─ store/
│  │  └─ types/
│  ├─ .env.example
│  └─ member.example.json
├─ frontend/
├─ data/
│  ├─ control.db                 # 全局账户和当前学期指针
│  ├─ semesters/<uuid>.db       # 各学期业务数据库
│  └─ work-study/templates/     # 所有学期共用的 Word 模板
├─ deploy/systemd/dms.service
├─ deploy/systemd/dms-backup.service
├─ deploy/systemd/dms-backup.timer
├─ build.sh / build.ps1
├─ backup.sh
├─ run.sh / run.ps1 / run.cmd
├─ update.sh
├─ clean.sh / clean.ps1 / clean.cmd
└─ README.md
```

## 数据库与首次迁移

新版本使用两层数据库：

- `data/control.db` 保存全局账户、密码哈希、学期目录和当前学期。
- `data/semesters/<uuid>.db` 保存某学期的成员姓名、角色、排班、工单、财务和劳务历史。

已有部署首次迁移时，`data/personnel.db` 和 `data/member.json` 只作为旧数据来源。迁移工具会创建名为 `2025-2026-2` 的首个学期，原文件及原财务目录不会被删除。

迁移前应停止服务并执行完整备份，然后运行：

```bash
./dms-migrate
```

迁移工具可重复运行；控制库已经初始化后只执行模式检查，不重复导入旧数据。

## 首次启动前需要准备的文件

### 1. 成员私有数据文件

新安装在控制库尚未创建时，可用本地私有文件提供首次成员名单：

- 默认路径：`data/member.json`
- 该文件已被 `.gitignore` 忽略，不会提交到 GitHub

可先复制模板再填写真实数据：

```bash
cp backend/member.example.json data/member.json
```

Windows PowerShell：

```powershell
Copy-Item backend/member.example.json data/member.json
```

### 2. 环境配置文件

构建脚本和启动脚本都会自动检查 `backend/.env`：

- 如果不存在，会从 `backend/.env.example` 自动复制一份
- 然后在终端提示你修改 `JWT_SECRET`

## JWT_SECRET 是做什么的

`JWT_SECRET` 是后端用来签名和校验登录令牌的密钥。

如果这个值泄露、被猜中，或者仍然使用默认值 `please-change-me`，就可能出现伪造登录状态的风险。因此：

- 不要把真实 `JWT_SECRET` 提交到 GitHub
- 线上环境不要继续使用默认值
- 每个部署环境都建议使用自己的随机密钥

## 常用环境变量

`backend/.env.example` 默认内容如下：

```env
APP_PORT=3000
CONTROL_DATABASE_PATH=../data/control.db
SEMESTER_DATABASE_DIR=../data/semesters
DATABASE_PATH=../data/personnel.db
PRIVATE_MEMBERS_PATH=../data/member.json
JWT_SECRET=please-change-me
DEFAULT_ADMIN_PASSWORD=admin
FIRST_MONDAY=20260302
WORK_STUDY_TEMPLATE_DIR=../data/work-study/templates
```

说明：

- `CONTROL_DATABASE_PATH` 和 `SEMESTER_DATABASE_DIR` 是正式运行数据位置
- `DATABASE_PATH`、`PRIVATE_MEMBERS_PATH` 和 `FIRST_MONDAY` 仅用于首次迁移旧系统
- `WORK_STUDY_TEMPLATE_DIR` 是所有学期共用的模板目录；导出的学期数据库不包含模板
- 启动脚本会先进入 `backend/` 再启动服务，所以 `../data/...` 会落到项目根目录下的 `data/`

## 启动方式

### Windows

构建：

```powershell
.\build.ps1
```

启动：

```powershell
.\run.ps1
```

或：

```cmd
run.cmd
```

### Linux

首次赋权：

```bash
chmod +x build.sh run.sh update.sh clean.sh backup.sh
```

构建：

```bash
./build.sh
```

启动：

```bash
./run.sh
```

说明：

- `run.sh` / `run.ps1` 会严格使用 `APP_PORT`
- 如果该端口已被占用，脚本会直接报错退出，不会静默切换到其他端口

## Linux 低配置服务器构建

`build.sh` 已针对低配置 Linux 机器做了优化。默认会在低内存或低 CPU 环境下自动启用低资源模式，降低 Go 和 Node 的并发与内存占用。

直接执行：

```bash
./build.sh
```

即可。

## dms 运维命令行

`build.sh` / `build.ps1` 会同时产出一个 `dms`（Windows 下为 `dms.exe`）运维工具，用于代替手工执行 update.sh、backup.sh 等脚本。旧脚本仍保留为兼容包装，行为等同于调用 dms。

```bash
./dms update                 # 拉取远端分支最新代码、重新构建并重启服务（失败自动恢复服务）
./dms update -branch main    # 指定分支；默认当前分支，游离 HEAD 时使用 main
./dms rollback               # 回退到上一次 update 之前的版本
./dms build                  # 仅构建
./dms start / stop / restart # 服务控制（有 systemd 时走 systemctl，否则直接进程模式）
./dms status                 # 服务状态、健康检查、数据概况
./dms logs -n 200 -f         # 查看服务日志（systemd 走 journalctl）
./dms backup                 # 备份控制库、学期库与模板；生成时间戳快照并更新 latest，可选 git 推送
./dms restore <快照目录>      # 从快照恢复（恢复前自动再备份当前数据）
./dms doctor                 # 体检：配置、依赖、端口、数据库完整性、备份目录
./dms version / dms env      # 版本信息 / 生效配置（敏感值脱敏）
```

说明：

- `dms` 通过向上查找 `backend/go.mod` 定位安装目录，也可以用 `DMS_HOME` 指定
- 系统环境变量优先级高于 `backend/.env`，便于临时覆盖单个配置
- `dms update` 默认管理 systemd 服务（`dms.service`）；设 `UPDATE_MANAGE_SERVICE=0` 可跳过
- 备份推送沿用 `backend/.env` 中的 `BACKUP_GIT_*` 配置；`-no-git` 可跳过推送

## Linux systemd 部署

项目使用单实例部署模式，推荐使用标准 `dms.service`。

仓库提供了 systemd 模板和安装脚本：

- [deploy/systemd/dms.service](deploy/systemd/dms.service)
- [deploy/systemd/install-systemd.sh](deploy/systemd/install-systemd.sh)

安装脚本会自动使用当前 Linux 用户、用户组和当前项目目录渲染服务文件。也可以用环境变量覆盖：

- `DMS_SERVICE_USER`
- `DMS_SERVICE_GROUP`
- `DMS_INSTALL_DIR`

安装方式：

```bash
bash deploy/systemd/install-systemd.sh
sudo systemctl enable --now dms.service
```

更新方式：

```bash
cd /opt/DMS
./update.sh
```

`update.sh` 会强制以远端仓库当前分支为准，覆盖本地已跟踪文件的改动，并清理会阻塞更新的未跟踪文件。被 `.gitignore` 忽略的本地私有文件（例如 `backend/.env`、`data/member.json`）不会被删除。

如果检测到 `dms.service` 正在运行，`update.sh` 还会自动执行：

- 停止 `dms.service`
- 拉取并覆盖代码
- 重新执行 `build.sh`
- 重新启动 `dms.service`

如果你只想更新代码而不管理服务，可临时关闭这个行为：

```bash
UPDATE_MANAGE_SERVICE=0 ./update.sh
```

## Linux 自动备份

项目提供了独立的自动备份脚本和 `systemd timer`，默认每天凌晨 `04:00` 自动执行一次备份。

默认行为：

- 使用 SQLite Backup API 备份 `data/control.db`
- 备份 `data/semesters/` 中的全部学期数据库
- 单独复制全局 `data/work-study/templates/` 模板目录
- 默认备份目录：`$HOME/DMS-backup`
- 定时触发时区：`Asia/Shanghai`（UTC+8）
- 每次执行会生成一个按时间戳命名的快照目录
- 同时更新一份 `latest/` 最新备份，方便快速恢复
- 本地备份成功后，默认使用 SSH 推送到 `git@github.com:Charles-0509/DMS-backup.git`

建议先为备份仓库生成专用 SSH key，并把公钥添加到 GitHub 私有仓库 `Charles-0509/DMS-backup` 的 Deploy keys 中，勾选写权限：

```bash
sudo -u charles ssh-keygen -t ed25519 -C "dms-backup@$(hostname)" -f /home/charles/.ssh/dms_backup_ed25519
sudo -u charles cat /home/charles/.ssh/dms_backup_ed25519.pub
```

在 `backend/.env` 中配置：

```bash
BACKUP_DIR=/home/charles/DMS-backup
BACKUP_GIT_ENABLED=1
BACKUP_GIT_REPO=git@github.com:Charles-0509/DMS-backup.git
BACKUP_GIT_BRANCH=main
BACKUP_SSH_KEY=/home/charles/.ssh/dms_backup_ed25519
BACKUP_GIT_AUTHOR_NAME="DMS Backup"
BACKUP_GIT_AUTHOR_EMAIL=dms-backup@localhost
```

测试 GitHub SSH 权限：

```bash
sudo -u charles GIT_SSH_COMMAND='ssh -i /home/charles/.ssh/dms_backup_ed25519 -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new' git ls-remote git@github.com:Charles-0509/DMS-backup.git
```

手动执行一次备份：

```bash
cd /opt/DMS
./backup.sh
```

安装自动备份：

```bash
bash deploy/systemd/install-systemd.sh
sudo systemctl enable --now dms-backup.timer
```

检查状态：

```bash
sudo systemctl status dms-backup.timer --no-pager
sudo systemctl list-timers dms-backup.timer --no-pager
```

恢复时应将数据库和全局模板分开处理：

- 恢复 `control.db` 与 `semesters/` 只恢复账户、学期目录和学期业务数据，不会覆盖服务器现有模板。
- `work-study/templates/` 属于服务器全局资源，只有在确认需要回退模板时才单独复制恢复。
- 不要把某个学期数据库当作模板备份；学期导出文件始终不包含 DOCX 模板。

## 清理本地构建产物

### Windows

```powershell
.\clean.ps1
```

或：

```cmd
clean.cmd
```

### Linux

```bash
./clean.sh
```

清理脚本只会删除构建产物，不会删除数据库、源码和 `node_modules`。

## 开发模式

### 前端

```bash
cd frontend
npm install
npm run dev
```

### 后端

如果你不通过启动脚本运行，而是直接开发启动：

```bash
cd backend
go run ./cmd/server
```

注意：这种方式不会自动加载 `.env`，需要自行准备环境变量。生产迁移应优先停服后运行 `dms-migrate`。
