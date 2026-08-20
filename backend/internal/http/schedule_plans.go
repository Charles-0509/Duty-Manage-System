package http

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

const schedulePlanImportMaxBytes int64 = 5 * 1024 * 1024

func (s *server) handleListSchedulePlans(c *gin.Context) {
	items, err := s.storeFor(c).ListSchedulePlans()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载排班表列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleGetSchedulePlan(c *gin.Context) {
	result, err := s.storeFor(c).GetSchedulePlan(c.Param("id"))
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleCreateSchedulePlan(c *gin.Context) {
	var request types.SaveSchedulePlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "排班表参数错误"})
		return
	}
	result, err := s.storeFor(c).CreateSchedulePlan(request.Name, request.Schedule)
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *server) handleUpdateSchedulePlan(c *gin.Context) {
	var request types.SaveSchedulePlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "排班表参数错误"})
		return
	}
	result, err := s.storeFor(c).UpdateSchedulePlan(c.Param("id"), request.Name, request.Schedule)
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleRenameSchedulePlan(c *gin.Context) {
	var request types.RenameSchedulePlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "排班表名称参数错误"})
		return
	}
	result, err := s.storeFor(c).RenameSchedulePlan(c.Param("id"), request.Name)
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleDeleteSchedulePlan(c *gin.Context) {
	if err := s.storeFor(c).DeleteSchedulePlan(c.Param("id")); err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "排班表已删除"})
}

func (s *server) handlePublishSchedulePlan(c *gin.Context) {
	result, err := s.storeFor(c).PublishSchedulePlan(c.Param("id"))
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleExportSchedulePlan(c *gin.Context) {
	filename, content, err := s.storeFor(c).ExportSchedulePlanWorkbook(c.Param("id"))
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleImportSchedulePlan(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, schedulePlanImportMaxBytes+64*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择新版 DMS 排班表 Excel"})
		return
	}
	if fileHeader.Size > schedulePlanImportMaxBytes || !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".xlsx") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持不超过 5MB 的 .xlsx 文件"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法读取排班表文件"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, schedulePlanImportMaxBytes+1))
	if err != nil || int64(len(content)) > schedulePlanImportMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "排班表文件读取失败或超过 5MB"})
		return
	}
	result, err := s.storeFor(c).ImportSchedulePlanWorkbook(c.PostForm("name"), content)
	if err != nil {
		writeSchedulePlanError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func writeSchedulePlanError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"message": "排班表不存在"})
	case errors.Is(err, store.ErrSchedulePlanNameConflict), errors.Is(err, store.ErrPublishedSchedulePlan):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	}
}
