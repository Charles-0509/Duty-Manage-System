# ShiftOS

`ShiftOS` 是一个面向值班团队的排班与工时协作平台，用来把“空闲时间收集、计划排班、实际值班确认、工单工时记录、人员权限管理”放进同一个系统里完成。

项目采用前后端分离架构：

- 前端：`Vue 3 + Vite + Pinia + Vue Router + Element Plus`
- 后端：`Golang + Gin + JWT`
- 数据库：`SQLite`

它适合小到中型团队在一台服务器上直接部署，开箱即可运行，也方便后续继续扩展接口或二次开发。

## 核心能力

- 值班人员登记单周、双周空闲时间
- 管理员根据空闲时间手动排班
- 按周生成并调整实际值班表
- 工单导入、工时记录、月度查询与导出
- 用户启停用、角色调整、密码重置
- 前后端一体化构建，最终只需运行一个 Go 进程

## 界面模块

- `值班时间登记`
  值班人员维护自己的单周/双周可值班时间，同时查看当前排班与全员空闲情况。
- `管理员排班`
  管理员查看全员空闲时间、手动安排班次、保存计划排班并导出 Excel。
- `实际值班调整`
  根据计划排班带出本周模板，再修正成真实值班结果。
- `工单管理`
  支持工单创建、编辑、删除、月度筛选、Excel 导出，以及从表格内容粘贴导入工时。
- `用户管理`
  支持账号状态维护、角色切换和密码重置。

## 目录结构

```text
PMS-GoVersion/
├─ backend/
│  ├─ cmd/server/main.go
│  ├─ internal/
│  │  ├─ config/             # 系统固定配置、角色权限、默认用户
│  │  ├─ http/               # 路由、接口、前端静态资源托管
│  │  ├─ store/              # SQLite 初始化、查询、导出
│  │  └─ types/              # API 数据结构
│  └─ go.mod
├─ frontend/
│  ├─ src/api/               # 前端请求封装
│  ├─ src/components/        # 通用组件
│  ├─ src/layouts/           # 页面布局
│  ├─ src/router/            # 路由与权限守卫
│  ├─ src/stores/            # 登录态与元配置
│  ├─ src/utils/             # 排班、导入导出工具
│  └─ src/views/             # 业务页面
├─ data/                     # 运行期 SQLite 数据库
├─ build.ps1                 # Windows 构建脚本
├─ run.ps1                   # Windows 启动脚本
├─ build.sh                  # Ubuntu/Linux 构建脚本
├─ run.sh                    # Ubuntu/Linux 启动脚本
└─ README.md
```

## 快速开始

### Windows

构建：

```powershell
cd PMS-GoVersion
.\build.ps1
```

运行：

```powershell
cd PMS-GoVersion
.\run.ps1
```

或者直接运行已构建好的可执行文件：

```powershell
cd PMS-GoVersion
.\personnel-management.exe
```

### Ubuntu / Linux

首次部署：

```bash
cd PMS-GoVersion
chmod +x build.sh run.sh
./build.sh
./run.sh
```

如果服务器未安装依赖：

```bash
sudo apt update
sudo apt install -y golang-go nodejs npm
```

## 开发方式

### 前端开发

```bash
cd PMS-GoVersion/frontend
npm install
npm run dev
```

默认地址：`http://localhost:5173`

### 后端开发

```bash
cd PMS-GoVersion/backend
go mod tidy
go run ./cmd/server
```

默认地址：`http://localhost:8080`

## 运行机制

- 前端构建结果会复制到 `backend/internal/http/web/dist/`
- Go 后端通过 `go:embed` 直接托管前端静态资源
- 生产环境下只需要部署数据库文件和一个 Go 可执行程序
- 默认数据库路径为 `data/personnel.db`
- 启动脚本会自动寻找可用端口，默认从 `8080` 开始

## 常用环境变量

```bash
APP_PORT=8080
DATABASE_PATH=./data/personnel.db
JWT_SECRET=please-change-me
DEFAULT_ADMIN_PASSWORD=admin
FIRST_MONDAY=20260302
GIN_MODE=release
```

## API 概览

### 认证

- `POST /api/auth/login`
- `GET /api/auth/me`
- `PUT /api/auth/password`

### 空闲时间与排班

- `GET /api/availability`
- `GET /api/availability/me`
- `PUT /api/availability/me`
- `GET /api/schedule`
- `PUT /api/schedule`
- `GET /api/final-schedules/:week`
- `PUT /api/final-schedules/:week`

### 工单

- `GET /api/work-orders?month=YYYY-MM`
- `POST /api/work-orders`
- `PUT /api/work-orders/:id`
- `DELETE /api/work-orders/:id`
- `GET /api/work-orders/export?month=YYYY-MM`

### 用户

- `GET /api/users`
- `PATCH /api/users/:id/role`
- `PATCH /api/users/:id/status`
- `PATCH /api/users/:id/password`

## 部署说明

- 适合部署在单机 Windows 或 Ubuntu 服务器
- 使用 SQLite，无需额外数据库服务
- 如果需要备份，只需定期备份 `data/personnel.db`
- 如果需要迁移，可直接复制整个项目目录到新机器
