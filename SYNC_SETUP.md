# DMS 数据库同步说明

本项目的业务数据本来就是统一写入 `DATABASE_PATH` 指定的 SQLite 数据库。
默认路径为：

```text
./data/personnel.db
```

## 你的网络拓扑下的推荐方案

你当前的网络条件是：

- 校园网内网服务器没有公网 IP，公网服务器无法主动访问它
- 校园网内网服务器可以访问公网服务器
- 公网服务器有公网 IPv4 和域名

在这种情况下，推荐使用：

- 校园网服务器 = 主写入源
- 公网服务器 = 被动接收同步的从库
- 每 5 分钟由校园网服务器主动把数据库快照推送到公网服务器

## 为什么不用双向同步

不要让两台都可写的 SQLite 节点每 5 分钟互相合并数据。

SQLite 文件同步适合：

- 单主写入
- 单向覆盖
- 定时同步副本

SQLite 文件同步不适合：

- 双写
- 多点写入
- 自动冲突合并

如果以后你需要真正的双写或多点写入，建议改成集中式 MySQL 或 PostgreSQL。

## 本次新增内容

- `GET /internal/db/snapshot`
  用于导出当前服务的一致性 SQLite 快照
- `POST /internal/db/import`
  用于接收外部推送的 SQLite 快照并导入本地数据库
- `backend/cmd/dbpush`
  用于把本地数据库快照主动推送到目标服务器
- `backend/cmd/dbsync`
  用于从远端拉取快照后导入本地数据库
  这个模式仍然保留，但不适合你现在的网络拓扑
- `sync-db.sh` 和 `sync-db.ps1`
  会根据环境变量自动选择“推送模式”或“拉取模式”

## 环境变量

两台服务器都建议配置：

```text
DATABASE_PATH=./data/personnel.db
SYNC_ENABLED=false
SYNC_TOKEN=change-me
```

在你当前的部署方式下：

校园网服务器需要：

```text
SYNC_TARGET_URL=https://your-public-server.example.com/internal/db/import
```

公网服务器不需要配置 `SYNC_TARGET_URL`，但需要在 DMS 服务上显式开启同步接口：

```text
SYNC_ENABLED=true
SYNC_TOKEN=change-me
```

如果以后有“可以主动访问主库”的环境，也可以改成拉取模式：

```text
SYNC_SOURCE_URL=https://your-campus-server.example.com/internal/db/snapshot
```

## 推荐部署方式

### 公网服务器

公网服务器正常启动 DMS 服务即可，并确保：

- `DATABASE_PATH=./data/personnel.db`
- `SYNC_ENABLED=true`
- `SYNC_TOKEN` 与校园网服务器保持一致
- 外部可访问 `POST /internal/db/import`

### 校园网服务器

校园网服务器作为主库，正常提供校内访问。
另外增加一个定时任务，每 5 分钟执行一次主动推送。

## Linux 示例

先在校园网服务器上构建推送程序：

```bash
cd /path/to/Duty-Manage-System/backend
go build -o ../db-push ./cmd/dbpush
```

然后在校园网服务器上通过 cron 每 5 分钟执行一次：

```cron
*/5 * * * * cd /path/to/Duty-Manage-System && DATABASE_PATH=./data/personnel.db SYNC_TOKEN=change-me SYNC_TARGET_URL=https://your-public-server.example.com/internal/db/import ./sync-db.sh >> /var/log/dms-db-sync.log 2>&1
```

## Windows 示例

如果校园网服务器是 Windows，也可以使用：

```powershell
$env:DATABASE_PATH="./data/personnel.db"
$env:SYNC_TOKEN="change-me"
$env:SYNC_TARGET_URL="https://your-public-server.example.com/internal/db/import"
.\sync-db.ps1
```

然后用任务计划程序每 5 分钟执行一次。

## 重要说明

如果公网服务器上也允许用户修改数据，那么下一次从校园网推送快照时，这些公网本地修改会被主库快照覆盖。

所以这套方案的前提是：

- 校园网服务器是主写入源
- 公网服务器主要用于只读访问或延迟副本访问
