// Command dms updates, operates, backs up, and restores Duty Manage System.
package main

import (
	"fmt"
	"os"
)

const usage = `dms - 机房管理系统运维工具

用法: dms <命令> [参数]

更新与构建:
  update     更新代码到远端分支最新版本并重新构建、重启服务（默认当前分支，无分支时 main）
             可选: -branch <分支>  -no-restart  -skip-build
  build      仅执行构建
  rollback   回退到上一次 update 之前的版本并重新构建、重启服务

服务控制:
  start      通过 systemd 启动服务并等待健康检查通过
  stop       停止服务
  restart    重启服务
  status     查看服务状态、健康检查与数据概况
  logs       查看服务日志，可选: -n <行数>（默认 100） -f（持续跟踪）

数据管理:
  backup     备份控制库、全部学期库与全局模板，生成时间戳快照并更新 latest
             可选: -out <目录>  -no-git（跳过 git 推送）
  restore    从备份快照恢复数据库（恢复前会自动再备份当前数据）
             用法: dms restore <快照目录> [-y] [-templates]
  sanitize   从当前数据库生成脱敏快照，不复制模板，不修改源数据
             用法: dms sanitize -out <目录>

诊断:
  version    显示版本信息（二进制构建信息 + 当前代码 git 版本）
  env        显示生效的运行配置（敏感值已脱敏）
  doctor     体检：环境、依赖、端口、数据库完整性、磁盘空间等

环境变量:
  DMS_HOME           指定安装目录（默认自动向上查找仓库根目录）

提示: 系统级环境变量优先级高于 backend/.env 中的同名配置。
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		if len(args) == 0 {
			os.Exit(2)
		}
		return
	}

	command := args[0]
	rest := args[1:]
	if len(rest) == 1 && (rest[0] == "-h" || rest[0] == "--help") {
		fmt.Print(usage)
		return
	}

	var err error
	switch command {
	case "update":
		err = runUpdate(rest)
	case "build":
		err = runBuild(rest)
	case "rollback":
		err = runRollback(rest)
	case "start":
		err = runStart(rest)
	case "stop":
		err = runStop(rest)
	case "restart":
		err = runRestart(rest)
	case "status":
		err = runStatus(rest)
	case "logs":
		err = runLogs(rest)
	case "backup":
		err = runBackup(rest)
	case "restore":
		err = runRestore(rest)
	case "sanitize":
		err = runSanitize(rest)
	case "version":
		err = runVersion(rest)
	case "env":
		err = runEnv(rest)
	case "doctor":
		err = runDoctor(rest)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", command)
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
