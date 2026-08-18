package http

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

const templateMaxUploadBytes int64 = 10 * 1024 * 1024

func (s *server) handleCreateUser(c *gin.Context) {
	var request types.CreateMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "成员参数不完整"})
		return
	}
	if err := s.storeFor(c).CreateSemesterMember(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, types.MessageResponse{Message: "成员已加入当前学期"})
}

func (s *server) handleUpdateUserProfile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "成员编号格式错误"})
		return
	}
	var request types.UpdateMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "成员参数错误"})
		return
	}
	if err := s.storeFor(c).UpdateSemesterMember(id, request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "成员资料已更新"})
}

func (s *server) handleRemoveUserMembership(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "成员编号格式错误"})
		return
	}
	if err := s.storeFor(c).RemoveSemesterMember(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "成员已移出当前学期"})
}

func (s *server) handleRestoreUserMembership(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "成员编号格式错误"})
		return
	}
	if err := s.storeFor(c).RestoreSemesterMember(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "成员已恢复到当前学期"})
}

func (s *server) handleGetTemplateStatus(c *gin.Context) {
	item, err := s.storeFor(c).WorkStudyTemplateStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载全局模板失败"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *server) handleUploadTemplate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, templateMaxUploadBytes+64*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil || !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".docx") || fileHeader.Size > templateMaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择不超过 10MB 的 DOCX 文件"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法读取模板文件"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, templateMaxUploadBytes+1))
	if err != nil || int64(len(content)) > templateMaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "模板读取失败或超过大小限制"})
		return
	}
	item, err := s.store.SaveWorkStudyTemplate(content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *server) handleDownloadTemplate(c *gin.Context) {
	filename, content, err := s.store.GetWorkStudyTemplate()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "模板不存在"})
		return
	}
	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", content)
}

func (s *server) handleDeleteTemplate(c *gin.Context) {
	if err := s.store.DeleteWorkStudyTemplate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "全局模板已删除"})
}
