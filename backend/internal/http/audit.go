package http

import (
	"net/http"
	"strconv"
	"strings"

	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

// auditRecorder logs authenticated write operations after they finish. It only
// records the method and path (never request bodies, which may contain
// passwords) plus the acting user, semester and client IP.
func (s *server) auditRecorder() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/auth/") {
			return
		}
		if _, ok := c.Get("current_user"); !ok {
			return
		}
		user := middleware.CurrentUser(c)

		activeValue, _ := c.Get("active_semester")
		active, _ := activeValue.(types.SemesterSummary)

		_ = s.store.InsertAuditLog(types.AuditLogEntry{
			Username:   user.Username,
			RealName:   user.RealName,
			Action:     c.Request.Method + " " + c.Request.URL.Path,
			Status:     c.Writer.Status(),
			SemesterID: active.ID,
			IP:         c.ClientIP(),
		})
	}
}

func (s *server) handleListAuditLogs(c *gin.Context) {
	page, err := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(strings.TrimSpace(c.Query("pageSize")))
	if err != nil || pageSize < 1 {
		pageSize = 50
	}

	result, err := s.store.ListAuditLogs(page, pageSize, c.Query("username"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载审计日志失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}
