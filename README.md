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
- 系统设置页面维护常用 `.env` 配置

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

## 首次启动前需要准备的文件

### 1. 成员私有数据文件

真实成员信息不再写在代码里，而是放在本地私有文件：

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
DATABASE_PATH=../data/personnel.db
PRIVATE_MEMBERS_PATH=../data/member.json
JWT_SECRET=please-change-me
DEFAULT_ADMIN_PASSWORD=admin
FIRST_MONDAY=20260302
SYNC_ENABLED=false
SYNC_TOKEN=change-me
SYNC_TARGET_URL=
SYNC_SOURCE_URL=
```

说明：

- `DATABASE_PATH` 和 `PRIVATE_MEMBERS_PATH` 是相对于 `backend/` 目录的路径
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

## Linux 低配置服务器构建

`build.sh` 已针对低配置 Linux 机器做了优化。默认会在低内存或低 CPU 环境下自动启用低资源模式，降低 Go 和 Node 的并发与内存占用。

直接执行：

```bash
./build.sh
```

即可。

## Linux systemd 部署

项目已经回归单实例部署模式，推荐使用标准 `dms.service`。

仓库提供了示例文件：

- [deploy/systemd/dms.service](deploy/systemd/dms.service)

默认示例假设：

- 部署目录：`/opt/DMS`
- 运行用户：`Charles`
- 运行组：`Charles`

安装方式：

```bash
sudo cp deploy/systemd/dms.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable dms.service
sudo systemctl start dms.service
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

- 备份数据库：`data/personnel.db`
- 备份成员名单：`data/member.json`
- 默认备份目录：`/home/Charles/DMS-backup`
- 每次执行会生成一个按时间戳命名的快照目录
- 同时更新一份 `latest/` 最新备份，方便快速恢复

手动执行一次备份：

```bash
cd /opt/DMS
./backup.sh
```

安装自动备份：

```bash
sudo cp deploy/systemd/dms-backup.service /etc/systemd/system/
sudo cp deploy/systemd/dms-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable dms-backup.timer
sudo systemctl start dms-backup.timer
```

检查状态：

```bash
sudo systemctl status dms-backup.timer --no-pager
sudo systemctl list-timers dms-backup.timer --no-pager
```

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

注意：这种方式不会自动帮你创建或加载 `.env`，需要你自己先准备好环境变量。
