package http

import (
	"net/http"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

func (s *server) handleGetSystemSettings(c *gin.Context) {
	settings, err := s.store.GetSemesterSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to load system settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

func (s *server) handleUpdateSystemSettings(c *gin.Context) {
	var request types.UpdateSystemSettingsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid system settings payload"})
		return
	}

	if err := s.store.UpdateSemesterSettings(request.FirstMonday, request.LaborSeed, request.WorkStudyContent); err != nil {
		status := http.StatusBadRequest
		if err == store.ErrArchivedSemester {
			status = http.StatusLocked
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "学期设置已保存并立即生效"})
}
