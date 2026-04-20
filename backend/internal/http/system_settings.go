package http

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

func (s *server) handleGetSystemSettings(c *gin.Context) {
	settings, err := config.LoadRuntimeSettings(s.cfg.EnvFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to load system settings"})
		return
	}

	c.JSON(http.StatusOK, types.SystemSettingsResponse{
		AppPort:               settings.AppPort,
		DatabasePath:          settings.DatabasePath,
		PrivateMembersPath:    settings.PrivateMembersPath,
		FirstMonday:           settings.FirstMonday,
		SyncEnabled:           settings.SyncEnabled,
		SyncToken:             settings.SyncToken,
		HotSlotBluePort:       settings.HotSlotBluePort,
		HotSlotGreenPort:      settings.HotSlotGreenPort,
		HotSwitchDrainSeconds: settings.HotSwitchDrainSeconds,
		EnvFilePath:           s.cfg.EnvFilePath,
		HotUpdateSupported:    runtime.GOOS == "linux" && fileExists(s.cfg.HotUpdateScriptPath),
	})
}

func (s *server) handleUpdateSystemSettings(c *gin.Context) {
	var request types.UpdateSystemSettingsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid system settings payload"})
		return
	}

	current, err := config.LoadRuntimeSettings(s.cfg.EnvFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to load system settings"})
		return
	}

	next := current
	next.DatabasePath = strings.TrimSpace(request.DatabasePath)
	next.PrivateMembersPath = strings.TrimSpace(request.PrivateMembersPath)
	next.FirstMonday = strings.TrimSpace(request.FirstMonday)
	next.SyncEnabled = request.SyncEnabled
	next.SyncToken = strings.TrimSpace(request.SyncToken)
	next.HotSwitchDrainSeconds = strings.TrimSpace(request.HotSwitchDrainSeconds)

	if err := validateEditableSystemSettings(next); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	if err := config.SaveRuntimeSettings(s.cfg.EnvFilePath, next); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to save system settings"})
		return
	}

	c.JSON(http.StatusOK, types.MessageResponse{Message: "system settings saved"})
}

func (s *server) handleTriggerHotUpdate(c *gin.Context) {
	if runtime.GOOS != "linux" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "hot update is only supported on Linux"})
		return
	}
	if !fileExists(s.cfg.HotUpdateScriptPath) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "hot-update.sh not found"})
		return
	}

	runtimeDir := filepath.Join(s.cfg.ProjectRoot, ".hot-runtime")
	lockDir := filepath.Join(runtimeDir, "web-deploy.lock")
	logDir := filepath.Join(runtimeDir, "logs")
	logPath := filepath.Join(logDir, "web-deploy.log")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to prepare hot-update runtime directory"})
		return
	}
	if err := os.Mkdir(lockDir, 0755); err != nil {
		if os.IsExist(err) {
			c.JSON(http.StatusConflict, gin.H{"message": "a hot update is already in progress"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to acquire hot-update lock"})
		return
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		_ = os.Remove(lockDir)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to open hot-update log"})
		return
	}

	command := exec.Command(
		"/bin/bash",
		"-lc",
		`trap 'rmdir "$DMS_WEB_DEPLOY_LOCK_DIR"' EXIT; "$DMS_HOT_UPDATE_SCRIPT" deploy`,
	)
	command.Dir = s.cfg.ProjectRoot
	command.Env = append(os.Environ(),
		"DMS_WEB_DEPLOY_LOCK_DIR="+lockDir,
		"DMS_HOT_UPDATE_SCRIPT="+s.cfg.HotUpdateScriptPath,
	)
	command.Stdout = logFile
	command.Stderr = logFile

	if err := command.Start(); err != nil {
		_ = logFile.Close()
		_ = os.Remove(lockDir)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to start hot update"})
		return
	}

	refreshDelay := 6
	if settings, err := config.LoadRuntimeSettings(s.cfg.EnvFilePath); err == nil {
		if drainSeconds, parseErr := parseNonNegativeInt(settings.HotSwitchDrainSeconds, "HOT_SWITCH_DRAIN_SECONDS"); parseErr == nil {
			refreshDelay = drainSeconds + 2
		}
	}

	go func() {
		_ = command.Wait()
		_ = logFile.Close()
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"message":      "hot update started",
		"refreshDelay": refreshDelay,
		"pollInterval": 2,
		"healthPath":   "/health",
	})
}

func validateEditableSystemSettings(settings config.RuntimeSettings) error {
	if strings.TrimSpace(settings.DatabasePath) == "" {
		return fmt.Errorf("DATABASE_PATH cannot be empty")
	}
	if strings.TrimSpace(settings.PrivateMembersPath) == "" {
		return fmt.Errorf("PRIVATE_MEMBERS_PATH cannot be empty")
	}
	if !isYYYYMMDD(settings.FirstMonday) {
		return fmt.Errorf("FIRST_MONDAY must use YYYYMMDD format")
	}
	if _, err := parseNonNegativeInt(settings.HotSwitchDrainSeconds, "HOT_SWITCH_DRAIN_SECONDS"); err != nil {
		return err
	}
	if settings.SyncEnabled && strings.TrimSpace(settings.SyncToken) == "" {
		return fmt.Errorf("SYNC_TOKEN is required when SYNC_ENABLED is true")
	}
	return nil
}

func isYYYYMMDD(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseNonNegativeInt(value string, field string) (int, error) {
	next, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || next < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", field)
	}
	return next, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
