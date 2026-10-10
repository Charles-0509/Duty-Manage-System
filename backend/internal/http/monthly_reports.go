package http

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"
)

func (s *server) registerMonthlyReports(group *gin.RouterGroup) {
	group.GET("/monthly-reports", s.handleMonthlyReports)
	group.POST("/monthly-reports/preview", s.handleMonthlyPreview)
	group.POST("/monthly-reports", s.handleMonthlyCreate)
	group.GET("/monthly-reports/:id", s.handleMonthlyDetail)
	group.POST("/monthly-reports/:id/versions", s.handleMonthlyVersion)
	group.POST("/monthly-reports/:id/check", s.handleMonthlyCheck)
	group.POST("/monthly-reports/:id/generate", s.handleMonthlyGenerate)
	group.POST("/monthly-reports/:id/finalize", s.handleMonthlyFinalize)
	group.DELETE("/monthly-reports/:id/versions/:versionId", s.handleMonthlyDelete)
	group.GET("/monthly-reports/:id/versions/:versionId/download/:kind", s.handleMonthlyDownload)
	group.GET("/monthly-reports/:id/sources/:sourceId/download/:kind", s.handleMonthlySourceDownload)
}

func (s *server) handleMonthlySourceDownload(c *gin.Context) {
	filename, data, err := s.storeFor(c).DownloadMonthlySource(c.Param("id"), c.Param("sourceId"), c.Param("kind"))
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	contentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	if strings.HasSuffix(filename, ".csv") {
		contentType = "text/csv; charset=utf-8"
	}
	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, contentType, data)
}

func bindMonthlyJSON(c *gin.Context, request any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	if err := c.ShouldBindJSON(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "月报参数无效"})
		return false
	}
	return true
}

func writeMonthlyError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "月报处理失败，请查看服务器日志"
	var input store.MonthlyInputError
	switch {
	case errors.As(err, &input):
		status = http.StatusBadRequest
	case errors.Is(err, sql.ErrNoRows):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrArchivedSemester):
		status = http.StatusLocked
	case errors.Is(err, store.ErrMonthlyConflict), errors.Is(err, store.ErrMonthlyReadOnly):
		status = http.StatusConflict
	}
	var validation store.MonthlyValidationError
	if errors.As(err, &validation) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"message": validation.Error(), "issues": validation.Issues})
		return
	}
	if status == http.StatusInternalServerError {
		log.Printf("monthly reports: %v", err)
	} else {
		message = err.Error()
	}
	c.JSON(status, gin.H{"message": message})
}

func (s *server) handleMonthlyReports(c *gin.Context) {
	items, err := s.storeFor(c).ListMonthlyReports()
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleMonthlyPreview(c *gin.Context) {
	var request types.MonthlyConditions
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).PreviewMonthlyReport(request)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleMonthlyCreate(c *gin.Context) {
	var request types.MonthlyReportRequest
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).CreateMonthlyReport(request)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *server) handleMonthlyDetail(c *gin.Context) {
	result, err := s.storeFor(c).GetMonthlyReport(c.Param("id"), c.Query("versionId"))
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleMonthlyVersion(c *gin.Context) {
	var request types.MonthlyVersionRequest
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).NewMonthlyVersion(c.Param("id"), request)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (s *server) handleMonthlyCheck(c *gin.Context) {
	var request types.MonthlyFinalizeRequest
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).CheckMonthlyReport(c.Param("id"), request.VersionID)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleMonthlyFinalize(c *gin.Context) {
	var request types.MonthlyFinalizeRequest
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).FinalizeMonthlyReport(c.Param("id"), request)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleMonthlyGenerate(c *gin.Context) {
	var request types.MonthlyFinalizeRequest
	if !bindMonthlyJSON(c, &request) {
		return
	}
	result, err := s.storeFor(c).GenerateMonthlyReport(c.Param("id"), request.VersionID)
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleMonthlyDelete(c *gin.Context) {
	if err := s.storeFor(c).DeleteMonthlyDraft(c.Param("id"), c.Param("versionId")); err != nil {
		writeMonthlyError(c, err)
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "草稿已删除"})
}

func (s *server) handleMonthlyDownload(c *gin.Context) {
	filename, data, err := s.storeFor(c).DownloadMonthlyFile(c.Param("id"), c.Param("versionId"), c.Param("kind"))
	if err != nil {
		writeMonthlyError(c, err)
		return
	}
	contentType := "application/zip"
	if strings.HasSuffix(filename, ".xlsx") {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	} else if strings.HasSuffix(filename, ".csv") {
		contentType = "text/csv; charset=utf-8"
	}
	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, contentType, data)
}
