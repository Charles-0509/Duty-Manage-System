# Go + Vue 重写版

这个目录是对原 `Streamlit + Python` 版本的完整重写实现：

- 前端：`Vue 3 + Vite + Pinia + Vue Router + Element Plus`
- 后端：`Golang + Gin + JWT + SQLite`
- 数据库：`SQLite`

## 目录结构

```text
Go_Version/
├─ backend/                  # Go REST API
│  ├─ cmd/server/main.go
│  ├─ internal/
│  │  ├─ config/             # 固定配置、人员名单、角色权限
│  │  ├─ http/               # 路由、控制器、中间件
│  │  ├─ store/              # SQLite 读写、初始化、导出
│  │  └─ types/              # API DTO
│  └─ .env.example
├─ frontend/                 # Vue 单页应用
│  ├─ src/api/               # Axios API 层
│  ├─ src/components/        # 通用组件
│  ├─ src/layouts/           # 主布局
│  ├─ src/router/            # 路由与权限守卫
│  ├─ src/stores/            # 登录态与元配置
│  ├─ src/utils/             # 排班/导出/导入工具
│  └─ src/views/             # 业务页面
└─ README.md
```

## 已实现功能

- 登录认证、JWT、首次登录强制修改密码
- 值班人员空闲时间登记
- 管理员计划排班
- HR/管理员实际值班表调整
- 工单与工时管理
- 用户管理、角色切换、启停用、密码重置
- 排班 Excel 导出
- 工单 Excel 导出

## 独立运行

如果你只是想直接运行这一版，不需要再分别启动前后端，先执行一次构建：

```bash
cd Go_Version
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

然后直接运行：

```bash
cd Go_Version
.\personnel-management.exe
```

或者用脚本直接本地开发启动：

```bash
cd Go_Version
.\run.cmd
```

脚本会自动把数据库放到 `Go_Version/data/personnel.db`，因此它脱离主项目根目录也能独立运行。

## 前端单独启动

```bash
cd Go_Version/frontend
cp .env.example .env
npm install
npm run dev
```

前端默认运行在 `http://localhost:5173`，并代理 `/api` 到 `http://localhost:8080`。

## 后端单独启动

先安装 Go 1.22+，然后执行：

```bash
cd Go_Version/backend
cp .env.example .env
go mod tidy
go run ./cmd/server
```

默认 API 地址：`http://localhost:8080`

## 当前运行模式

- 构建后的前端静态资源会被复制到 `backend/internal/http/web/dist/`
- Go 后端会直接托管前端页面和 `/api/*` 接口
- 因此最终可以只运行一个 Go 进程，不再依赖 Vite 开发服务器

## 主要接口

### 认证

- `POST /api/auth/login`
- `GET /api/auth/me`
- `PUT /api/auth/password`

### 值班与排班

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

## 说明

- 这版为了避免破坏原项目，代码落在 `Go_Version/` 下，没有覆盖原 Python 版本。
- `run.ps1` / `run.cmd` 会把默认数据库写到 `Go_Version/data/`，因此复制整个 `Go_Version` 目录到别处后仍可独立运行。
