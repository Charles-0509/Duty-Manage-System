# 机房管理系统

机房管理系统是一个面向机房运维团队的排班、工单、财务统计和用户管理平台。项目采用前后端一体化部署方式：前端构建后嵌入 Go 后端，最终只需要运行一个服务进程。

## 技术栈

- 前端：Vue 3 + Vite + Pinia + Vue Router + Element Plus
- 后端：Go + Gin + SQLite
- 鉴权：JWT

## 主要功能

- 值班人员登记单双周可值班时间
- 管理员安排计划排班
- 按周生成并调整实际值班表
- 工单管理、工时记录、Excel 导出
- 财务统计与 Excel 导出
- 用户角色、账号状态、密码管理

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
├─ build.sh / build.ps1
├─ run.sh / run.ps1 / run.cmd
├─ hot-update.sh
├─ clean.sh / clean.ps1 / clean.cmd
├─ HOT_UPDATE.md
└─ README.md
```

## 首次启动前需要准备的文件

### 1. 成员私有数据文件

真实成员信息不再写在代码中，而是放在本地私有文件：

- 默认路径：`data/member.json`
- 该文件已被 `.gitignore` 忽略，不会提交到 GitHub

你可以把模板文件复制过去再填写真实数据：

```bash
cp backend/member.example.json data/member.json
```

Windows PowerShell：

```powershell
Copy-Item backend/member.example.json data/member.json
```

### 2. 环境配置文件

构建脚本和启动脚本都会自动检查 `backend/.env`：

- 如果不存在，会自动从 `backend/.env.example` 复制一份
- 然后在终端提示你修改 `JWT_SECRET`

也就是说，第一次执行 `build.sh`、`build.ps1`、`run.sh` 或 `run.ps1` 时，不需要手动先创建 `.env`。

## JWT_SECRET 是做什么的

`JWT_SECRET` 是后端用来签名和校验登录令牌的密钥。

作用是：

- 用户登录后，服务端会生成一个 JWT 令牌
- 这个令牌会用 `JWT_SECRET` 进行签名
- 之后服务端再用同一个密钥验证令牌是否真实、是否被篡改

如果这个值使用默认值、泄露，或者被别人猜到，就可能出现伪造登录状态的问题。所以：

- 不要在公开仓库里提交真实 `JWT_SECRET`
- 不要在线上环境继续使用 `please-change-me`
- 每个部署环境最好使用自己的随机密钥

## 常用环境变量

`backend/.env.example` 当前默认内容如下：

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

`build.ps1` 会优先读取 `backend/.env`，如果文件不存在，会先自动从 `backend/.env.example` 生成。

启动：

```powershell
.\run.ps1
```

或者：

```cmd
run.cmd
```

### Linux

首次赋权：

```bash
chmod +x build.sh run.sh clean.sh
```

构建：

```bash
./build.sh
```

`build.sh` 会优先读取 `backend/.env`，如果文件不存在，会先自动从 `backend/.env.example` 生成。

启动：

```bash
./run.sh
```

## 低配置云服务器构建

`build.sh` 已针对低配置 Linux 机器做过优化。默认会在低内存或单核环境下自动开启低资源模式，降低 Go 和 Node 的并发与内存占用。

直接执行：

```bash
./build.sh
```

即可。

## Linux 不停机更新

如果你希望 Linux 服务器更新时不中断服务，可以使用：

```bash
./hot-update.sh start
```

后续更新时执行：

```bash
git pull
./hot-update.sh deploy
```

完整说明见 [HOT_UPDATE.md](C:/Users/Charles/Desktop/Duty-Manage-System/HOT_UPDATE.md)。

如果你希望服务器开机自动拉起这套蓝绿热更新栈，仓库里也提供了 systemd 单元文件：

- [deploy/systemd/dms-hot-update.service](C:/Users/Charles/Desktop/Duty-Manage-System/deploy/systemd/dms-hot-update.service)
- [deploy/systemd/dms-hot-update-deploy.service](C:/Users/Charles/Desktop/Duty-Manage-System/deploy/systemd/dms-hot-update-deploy.service)

默认示例中的部署目录是 `/opt/DMS`。

## 清理本地构建产物

### Windows

```powershell
.\clean.ps1
```

或者：

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

## 额外说明

- 私有成员数据说明见 [PRIVATE_DATA_SETUP.md](C:/Users/Charles/Desktop/Duty-Manage-System/PRIVATE_DATA_SETUP.md)
- 如果仓库历史中曾提交过真实姓名或其他敏感信息，建议进一步清理 Git 历史
