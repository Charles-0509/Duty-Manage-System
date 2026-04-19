package http

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type server struct {
	cfg   config.AppConfig
	store *store.Store
}

func NewRouter(cfg config.AppConfig, appStore *store.Store) *gin.Engine {
	s := &server{cfg: cfg, store: appStore}

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	if cfg.SyncEnabled {
		router.GET("/internal/db/snapshot", s.handleDatabaseSnapshot)
		router.POST("/internal/db/import", s.handleDatabaseImport)
	}

	api := router.Group("/api")
	api.POST("/auth/login", s.handleLogin)

	authGroup := api.Group("")
	authGroup.Use(middleware.Auth(cfg.JWTSecret, appStore))
	authGroup.GET("/auth/me", s.handleMe)
	authGroup.PUT("/auth/password", s.handleChangePassword)
	authGroup.GET("/meta/config", s.handleMetaConfig)
	authGroup.GET("/dashboard", s.handleDashboard)
	authGroup.GET("/availability", s.handleAvailabilityOverview)
	authGroup.GET("/availability/me", s.handleMyAvailability)
	authGroup.PUT("/availability/me", s.handleSaveAvailability)
	authGroup.GET("/schedule", s.handleSchedule)
	authGroup.GET("/final-schedules/:week", s.handleFinalSchedule)
	authGroup.GET("/work-orders", s.handleListWorkOrders)
	authGroup.GET("/work-orders/export", middleware.RequireRoles("ADMIN", "HR"), s.handleExportWorkOrders)

	adminGroup := authGroup.Group("")
	adminGroup.Use(middleware.RequireRoles("ADMIN"))
	adminGroup.GET("/availability/users/:username", s.handleUserAvailability)
	adminGroup.PUT("/availability/users/:username", s.handleSaveUserAvailability)
	adminGroup.PUT("/schedule", s.handleSaveSchedule)
	adminGroup.GET("/schedule/export", s.handleExportSchedule)
	adminGroup.POST("/work-orders", s.handleCreateWorkOrder)
	adminGroup.PUT("/work-orders/:id", s.handleUpdateWorkOrder)
	adminGroup.DELETE("/work-orders/:id", s.handleDeleteWorkOrder)
	adminGroup.GET("/users", s.handleUsers)
	adminGroup.PATCH("/users/:id/role", s.handleUpdateRole)
	adminGroup.PATCH("/users/:id/status", s.handleUpdateUserStatus)
	adminGroup.PATCH("/users/:id/password", s.handleResetPassword)

	finalScheduleGroup := authGroup.Group("")
	finalScheduleGroup.Use(middleware.RequireRoles("ADMIN", "HR"))
	finalScheduleGroup.PUT("/final-schedules/:week", s.handleSaveFinalSchedule)

	registerFrontendRoutes(router)

	return router
}

func (s *server) handleLogin(c *gin.Context) {
	var request types.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "登录参数不完整"})
		return
	}

	user, err := s.store.Authenticate(strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
		return
	}

	token, err := s.generateToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "生成登录令牌失败"})
		return
	}

	c.JSON(http.StatusOK, types.LoginResponse{
		Token: token,
		User:  *user,
	})
}

func (s *server) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, middleware.CurrentUser(c))
}

func (s *server) handleChangePassword(c *gin.Context) {
	var request types.ChangePasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "密码参数不完整"})
		return
	}

	if strings.TrimSpace(request.NewPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码不能为空"})
		return
	}

	user := middleware.CurrentUser(c)
	if err := s.store.UpdateOwnPassword(user.ID, request.CurrentPassword, request.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	updatedUser, _ := s.store.GetUserByID(user.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "密码修改成功",
		"user":    updatedUser,
	})
}

func (s *server) handleMetaConfig(c *gin.Context) {
	c.JSON(http.StatusOK, types.MetaConfigResponse{
		WeekdaysCode:    config.WeekdaysCode,
		WeekdaysDisplay: config.WeekdaysDisplay,
		TimeSlots:       config.TimeSlots,
		UserNames:       config.UserNames,
		UserRoles:       config.UserRoles,
		RolePermissions: config.RolePermissions,
		FirstMonday:     s.cfg.FirstMonday,
	})
}

func (s *server) handleDashboard(c *gin.Context) {
	data, err := s.store.GetDashboard()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载首页数据失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleAvailabilityOverview(c *gin.Context) {
	data, err := s.store.GetAvailabilityOverview()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载空闲时间失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": data})
}

func (s *server) handleMyAvailability(c *gin.Context) {
	user := middleware.CurrentUser(c)
	data, err := s.store.GetAvailabilityForUser(user.RealName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载个人空闲时间失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleSaveAvailability(c *gin.Context) {
	var request types.SaveAvailabilityRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "空闲时间参数错误"})
		return
	}

	user := middleware.CurrentUser(c)
	if err := s.store.SaveAvailability(user.RealName, request); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "保存空闲时间失败"})
		return
	}

	c.JSON(http.StatusOK, types.MessageResponse{Message: "空闲时间已保存"})
}

func (s *server) handleUserAvailability(c *gin.Context) {
	user, err := s.store.GetUserByUsername(strings.TrimSpace(c.Param("username")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}

	data, err := s.store.GetAvailabilityForUser(user.RealName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载用户空闲时间失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleSaveUserAvailability(c *gin.Context) {
	var request types.SaveAvailabilityRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "空闲时间参数错误"})
		return
	}

	user, err := s.store.GetUserByUsername(strings.TrimSpace(c.Param("username")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}

	if err := s.store.SaveAvailability(user.RealName, request); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "保存用户空闲时间失败"})
		return
	}

	c.JSON(http.StatusOK, types.MessageResponse{Message: "用户空闲时间已保存"})
}

func (s *server) handleSchedule(c *gin.Context) {
	data, err := s.store.GetScheduleSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载排班失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleSaveSchedule(c *gin.Context) {
	var request types.SaveScheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "排班参数错误"})
		return
	}

	if err := s.store.SaveSchedule(request.Schedule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "保存排班失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "排班已保存"})
}

func (s *server) handleFinalSchedule(c *gin.Context) {
	weekNumber, err := strconv.Atoi(c.Param("week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "周数格式错误"})
		return
	}

	selectedDate := c.Query("date")
	if selectedDate == "" {
		selectedDate = time.Now().Format("2006-01-02")
	}

	data, err := s.store.GetFinalSchedule(weekNumber, selectedDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载实际值班表失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleSaveFinalSchedule(c *gin.Context) {
	weekNumber, err := strconv.Atoi(c.Param("week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "周数格式错误"})
		return
	}

	var request types.SaveFinalScheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实际值班参数错误"})
		return
	}

	if strings.TrimSpace(request.SelectedDate) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择日期"})
		return
	}

	user := middleware.CurrentUser(c)
	if err := s.store.SaveFinalSchedule(weekNumber, request, user.RealName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "保存实际值班表失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "实际值班表已保存"})
}

func (s *server) handleListWorkOrders(c *gin.Context) {
	month := c.Query("month")
	items, err := s.store.ListWorkOrders(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载工单失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleCreateWorkOrder(c *gin.Context) {
	var request types.SaveWorkOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单参数错误"})
		return
	}

	user := middleware.CurrentUser(c)
	workOrder, err := s.store.CreateWorkOrder(request, user.RealName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, workOrder)
}

func (s *server) handleUpdateWorkOrder(c *gin.Context) {
	var request types.SaveWorkOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单参数错误"})
		return
	}

	workOrder, err := s.store.UpdateWorkOrder(c.Param("id"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, workOrder)
}

func (s *server) handleDeleteWorkOrder(c *gin.Context) {
	if err := s.store.DeleteWorkOrder(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "删除工单失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "工单已删除"})
}

func (s *server) handleExportSchedule(c *gin.Context) {
	content, err := s.store.ExportScheduleWorkbook()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出排班失败"})
		return
	}

	c.Header("Content-Disposition", `attachment; filename="schedule.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleExportWorkOrders(c *gin.Context) {
	month := c.Query("month")
	content, err := s.store.ExportWorkOrdersWorkbook(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出工单失败"})
		return
	}

	filename := "work-orders.xlsx"
	if month != "" {
		filename = fmt.Sprintf("work-orders-%s.xlsx", month)
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleUsers(c *gin.Context) {
	users, err := s.store.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载用户失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": users})
}

func (s *server) handleUpdateRole(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "用户编号格式错误"})
		return
	}

	var request types.UpdateRoleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "角色参数错误"})
		return
	}

	if err := s.store.UpdateRole(userID, request.Role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "角色更新成功"})
}

func (s *server) handleUpdateUserStatus(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "用户编号格式错误"})
		return
	}

	var request types.UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "状态参数错误"})
		return
	}

	if err := s.store.UpdateUserStatus(userID, request.IsActive); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "更新用户状态失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "用户状态已更新"})
}

func (s *server) handleResetPassword(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "用户编号格式错误"})
		return
	}

	var request types.AdminResetPasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "密码参数错误"})
		return
	}

	if strings.TrimSpace(request.NewPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码不能为空"})
		return
	}

	if err := s.store.ResetPassword(userID, request.NewPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "重置密码失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "密码已重置，下次登录将强制修改"})
}

func (s *server) handleDatabaseSnapshot(c *gin.Context) {
	if !s.hasValidSyncToken(c.GetHeader("X-Sync-Token")) {
		c.JSON(http.StatusForbidden, gin.H{"message": "sync token invalid"})
		return
	}

	tempFile, err := os.CreateTemp("", "dms-db-snapshot-*.db")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to allocate snapshot file"})
		return
	}

	snapshotPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(snapshotPath)

	if err := s.store.CreateSnapshot(snapshotPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to create snapshot"})
		return
	}

	c.FileAttachment(snapshotPath, "personnel.db")
}

func (s *server) handleDatabaseImport(c *gin.Context) {
	if !s.hasValidSyncToken(c.GetHeader("X-Sync-Token")) {
		c.JSON(http.StatusForbidden, gin.H{"message": "sync token invalid"})
		return
	}

	tempFile, err := os.CreateTemp("", "dms-db-import-*.db")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to allocate import file"})
		return
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, c.Request.Body); err != nil {
		tempFile.Close()
		c.JSON(http.StatusBadRequest, gin.H{"message": "failed to read import payload"})
		return
	}

	if err := tempFile.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to finalize import payload"})
		return
	}

	if err := s.store.ImportSnapshot(tempPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to import snapshot"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "snapshot imported"})
}

func (s *server) hasValidSyncToken(token string) bool {
	expected := strings.TrimSpace(s.cfg.SyncToken)
	token = strings.TrimSpace(token)

	if expected == "" || token == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

func (s *server) generateToken(userID int64) (string, error) {
	claims := middleware.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   strconv.FormatInt(userID, 10),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}
