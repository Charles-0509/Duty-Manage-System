package http

import (
	"math"
	"net/http"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

func (s *server) handleGetSystemSettings(c *gin.Context) {
	settings, err := s.storeFor(c).GetSemesterSettings()
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

	rates := store.RateConfig{
		DutyCents:       yuanToCents(request.DutyRate),
		WorkOrderCents:  yuanToCents(request.WorkOrderRate),
		MgmtLeaderCents: yuanToCents(request.MgmtLeaderRate),
		MgmtOwnerCents:  yuanToCents(request.MgmtOwnerRate),
	}

	if err := s.storeFor(c).UpdateSemesterSettings(request.FirstMonday, request.WorkStudyContent, rates); err != nil {
		status := http.StatusBadRequest
		if err == store.ErrArchivedSemester {
			status = http.StatusLocked
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "学期设置已保存并立即生效"})
}

func yuanToCents(value float64) int64 {
	return int64(math.Round(value * 100))
}
