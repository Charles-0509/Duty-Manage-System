package http

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

const semesterImportMaxBytes int64 = 100 * 1024 * 1024

func (s *server) semesterWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/api/auth/password" || strings.HasSuffix(path, "/password") || strings.HasSuffix(path, "/status") || strings.HasPrefix(path, "/api/templates") {
			c.Next()
			return
		}
		active, _ := c.Get("active_semester")
		semester, _ := active.(types.SemesterSummary)
		if semester.Archived {
			c.JSON(http.StatusLocked, gin.H{"message": store.ErrArchivedSemester.Error()})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *server) handleListSemesters(c *gin.Context) {
	items, err := s.store.ListSemesters()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载学期列表失败"})
		return
	}
	active := s.store.ActiveSemester()
	c.Header("X-DMS-Semester-ID", active.ID)
	c.Header("X-DMS-Context-Version", strconv.FormatInt(active.ContextVersion, 10))
	c.JSON(http.StatusOK, gin.H{"items": items, "active": active})
}

func (s *server) handleCreateSemester(c *gin.Context) {
	var request types.CreateSemesterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "学期参数不完整"})
		return
	}
	item, err := s.store.CreateSemester(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (s *server) handleActivateSemester(c *gin.Context) {
	item, err := s.store.ActivateSemester(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.Header("X-DMS-Semester-ID", item.ID)
	c.Header("X-DMS-Context-Version", strconv.FormatInt(item.ContextVersion, 10))
	c.JSON(http.StatusOK, item)
}

func (s *server) handleArchiveSemester(c *gin.Context) {
	if err := s.store.SetSemesterArchived(c.Param("id"), true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "学期已归档"})
}

func (s *server) handleUnarchiveSemester(c *gin.Context) {
	if err := s.store.SetSemesterArchived(c.Param("id"), false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "学期已解除归档"})
}

func (s *server) handleUpdateSemester(c *gin.Context) {
	var request types.UpdateSemesterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "学期参数错误"})
		return
	}
	if err := s.store.UpdateSemesterName(c.Param("id"), request.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "学期名称已更新"})
}

func (s *server) handleDeleteSemester(c *gin.Context) {
	if err := s.store.DeleteDraftSemester(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "草稿学期已删除"})
}

func (s *server) handleExportSemester(c *gin.Context) {
	filename, content, err := s.store.ExportSemester(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "导出学期数据库失败"})
		return
	}
	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/vnd.sqlite3", content)
}

func (s *server) handleImportSemester(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, semesterImportMaxBytes+1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择学期数据库文件"})
		return
	}
	if fileHeader.Size > semesterImportMaxBytes || !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".db") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持不超过 100MB 的 .db 文件"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法读取数据库文件"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, semesterImportMaxBytes+1))
	if err != nil || int64(len(content)) > semesterImportMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "数据库文件读取失败或超过大小限制"})
		return
	}
	item, err := s.store.ImportSemester(content)
	if err != nil {
		if errors.Is(err, store.ErrArchivedSemester) {
			c.JSON(http.StatusLocked, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}
