package http

import (
	"net/http"

	"personnel-management-go/internal/http/middleware"

	"github.com/gin-gonic/gin"
)

func (s *server) handlePersonalRecords(c *gin.Context) {
	user := middleware.CurrentUser(c)
	records, err := s.storeFor(c).GetPersonalRecords(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载个人记录失败"})
		return
	}
	c.JSON(http.StatusOK, records)
}

func (s *server) handleExportPersonalRecords(c *gin.Context) {
	user := middleware.CurrentUser(c)
	content, err := s.storeFor(c).ExportPersonalRecordsWorkbook(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出个人记录失败"})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="my-records.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}
