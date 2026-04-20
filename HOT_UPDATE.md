# Linux 不停机更新

当前项目已经补了一套适用于 Linux 单机部署的“不停机更新”方案，核心思路是：

- 保持一个稳定入口端口
- 后台同时维护两个应用槽位：`blue` / `green`
- 新版本先在空闲槽位启动并通过健康检查
- 再把稳定入口切到新槽位
- 等待旧请求自然结束后，再停止旧槽位

这样更新时不会再经历：

```text
stop service -> git pull -> build -> start service
```

导致的服务中断窗口。

## 组成

- [hot-update.sh](C:/Users/Charles/Desktop/Duty-Manage-System/hot-update.sh)
  - `start`：首次启动蓝绿栈
  - `deploy`：发布新版本并无停机切换
  - `status`：查看当前状态
  - `stop`：停止整套蓝绿栈
- `backend/cmd/hotproxy`
  - 负责监听对外固定端口
  - 把流量转发到当前激活槽位
- 后端主程序
  - 已支持 `SIGTERM` 优雅停机
  - 配合切换脚本可平滑退出

## 前提

1. 已准备好 `backend/.env`
2. 已准备好 `data/member.json`
3. Linux 上已安装：
   - `go`
   - `npm`
   - `curl`
   - `python3`（若系统提供 `python` 也可）

## 首次切换到热更新模式

如果你当前还是传统单进程运行方式：

1. 先停止旧的单实例服务
2. 在项目根目录执行：

```bash
chmod +x build.sh run.sh hot-update.sh
./hot-update.sh start
```

首次启动后：

- 稳定入口端口仍然使用 `backend/.env` 里的 `APP_PORT`
- 蓝绿槽位默认端口为：
  - `blue`: `18081`
  - `green`: `18082`

## systemd 开机自启

仓库里已经提供了 systemd 单元文件：

- [deploy/systemd/dms-hot-update.service](C:/Users/Charles/Desktop/Duty-Manage-System/deploy/systemd/dms-hot-update.service)
- [deploy/systemd/dms-hot-update-deploy.service](C:/Users/Charles/Desktop/Duty-Manage-System/deploy/systemd/dms-hot-update-deploy.service)

这两份文件默认假设：

- 项目部署目录：`/opt/DMS`
- 运行用户：`Charles`
- 运行用户组：`Charles`

如果你的服务器实际不是这套路径或用户，需要先修改单元文件中的：

- `User=`
- `Group=`
- `WorkingDirectory=`
- `ExecStart=`
- `ExecStop=`

### 安装步骤

```bash
sudo cp deploy/systemd/dms-hot-update.service /etc/systemd/system/
sudo cp deploy/systemd/dms-hot-update-deploy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable dms-hot-update.service
sudo systemctl start dms-hot-update.service
```

### 开机自动启动

启用后，系统重启时会自动执行：

```bash
/opt/DMS/hot-update.sh start
```

`hot-update.sh start` 已做成幂等逻辑：

- 如果当前没有激活槽位，会初始化蓝绿栈
- 如果已有激活槽位，会恢复该槽位并拉起热代理

### 发布新版本

代码更新后可以手动执行：

```bash
sudo systemctl start dms-hot-update-deploy.service
```

这条命令只负责执行 `hot-update.sh deploy`。通常建议配合：

```bash
cd /opt/DMS
git pull
sudo systemctl start dms-hot-update-deploy.service
```

## 日常更新

后续更新流程改成：

```bash
git pull
./hot-update.sh deploy
```

脚本会自动完成：

1. 构建新版本
2. 启动到空闲槽位
3. 检查 `http://127.0.0.1:<slot-port>/health`
4. 切换稳定入口到新槽位
5. 等待旧请求排空
6. 停止旧槽位

## 状态查看

```bash
./hot-update.sh status
```

## 停止

```bash
./hot-update.sh stop
```

## 关键环境变量

可选配置：

```env
APP_PORT=3000
HOT_SLOT_BLUE_PORT=18081
HOT_SLOT_GREEN_PORT=18082
HOT_SWITCH_DRAIN_SECONDS=5
```

说明：

- `APP_PORT`：外部访问端口，由热代理监听
- `HOT_SLOT_BLUE_PORT` / `HOT_SLOT_GREEN_PORT`：两个后台实例的内部端口
- `HOT_SWITCH_DRAIN_SECONDS`：切换后保留旧实例的排空时间

如果未在环境里显式设置 `HOT_SLOT_BLUE_PORT` / `HOT_SLOT_GREEN_PORT`，脚本会使用默认值。

## SQLite 说明

这套方案会在切换期间短暂存在两个后端实例同时访问同一个 SQLite 数据库，因此代码里已经做了这两件事：

- 启用 `WAL` 模式
- 设置 `busy_timeout`

这样在蓝绿切换的短时间重叠期里更稳妥。

## 风险边界

这套方案适合当前项目这种中小规模、单机、SQLite 场景。它不是多机集群方案。

需要明确：

- 真正的“绝对零中断”仍取决于客户端请求时序
- 但相比旧的停机更新方式，中断窗口已经基本消除
- 如果以后并发写入明显增大，建议把数据库升级到 MySQL/PostgreSQL
