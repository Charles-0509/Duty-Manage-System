package http

import (
	"fmt"
	"net/http"
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
		AppPort:            settings.AppPort,
		DatabasePath:       settings.DatabasePath,
		PrivateMembersPath: settings.PrivateMembersPath,
		FirstMonday:        settings.FirstMonday,
		SyncEnabled:        settings.SyncEnabled,
		SyncToken:          settings.SyncToken,
		EnvFilePath:        s.cfg.EnvFilePath,
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
