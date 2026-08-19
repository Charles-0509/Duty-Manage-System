package http

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type server struct {
	cfg          config.AppConfig
	store        *store.Store
	loginLimiter *middleware.RateLimiter
}

const requestStoreKey = "request_store"

// storeFor returns the per-request store snapshot captured by the semester
// context middleware, so handlers keep using the same database handles even if
// an admin switches the active semester mid-request.
func (s *server) storeFor(c *gin.Context) *store.Store {
	return c.MustGet(requestStoreKey).(*store.Store)
}

func NewRouter(cfg config.AppConfig, appStore *store.Store) *gin.Engine {
	s := &server{
		cfg:          cfg,
		store:        appStore,
		loginLimiter: middleware.NewRateLimiter(5, 5*time.Minute, 5*time.Minute),
	}

	router := gin.Default()
	router.RemoteIPHeaders = []string{"CF-Connecting-IP"}
	if err := router.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		log.Printf("failed to configure trusted cloudflared proxies: %v", err)
	}
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		c.Next()
	})
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Disposition", "X-DMS-Semester-ID", "X-DMS-Context-Version"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	api := router.Group("/api")
	api.POST("/auth/login", s.handleLogin)
	api.POST("/auth/refresh", s.handleRefreshToken)

	semesterAPI := api.Group("/semesters")
	semesterAPI.Use(middleware.AuthGlobal(cfg.JWTSecret, appStore), middleware.RequirePasswordChange(), middleware.RequireRoles("ADMIN"), s.auditRecorder())
	semesterAPI.GET("", s.handleListSemesters)
	semesterAPI.POST("", s.handleCreateSemester)
	semesterAPI.POST("/import", s.handleImportSemester)
	semesterAPI.GET("/:id/export", s.handleExportSemester)
	semesterAPI.POST("/:id/activate", s.handleActivateSemester)
	semesterAPI.POST("/:id/archive", s.handleArchiveSemester)
	semesterAPI.POST("/:id/unarchive", s.handleUnarchiveSemester)
	semesterAPI.PATCH("/:id", s.handleUpdateSemester)
	semesterAPI.DELETE("/:id", s.handleDeleteSemester)

	auditAPI := api.Group("/audit-logs")
	auditAPI.Use(middleware.AuthGlobal(cfg.JWTSecret, appStore), middleware.RequirePasswordChange(), middleware.RequireRoles("ADMIN"))
	auditAPI.GET("", s.handleListAuditLogs)

	authGroup := api.Group("")
	authGroup.Use(func(c *gin.Context) {
		requestStore, release, err := appStore.AcquireRequest()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "加载学期配置失败"})
			c.Abort()
			return
		}
		defer release()
		active := requestStore.ActiveSemester()
		c.Set("active_semester", active)
		c.Set(requestStoreKey, requestStore)
		c.Header("X-DMS-Semester-ID", active.ID)
		c.Header("X-DMS-Context-Version", strconv.FormatInt(active.ContextVersion, 10))
		c.Next()
	})
	authGroup.Use(middleware.Auth(cfg.JWTSecret, appStore))
	authGroup.Use(middleware.RequirePasswordChange())
	authGroup.Use(s.auditRecorder())
	authGroup.Use(s.semesterWriteGuard())
	authGroup.POST("/auth/logout", s.handleLogout)
	authGroup.GET("/auth/me", s.handleMe)
	authGroup.PUT("/auth/password", s.handleChangePassword)
	authGroup.GET("/meta/config", s.handleMetaConfig)
	authGroup.GET("/dashboard", s.handleDashboard)
	authGroup.GET("/my-records", s.handlePersonalRecords)
	authGroup.GET("/my-records/export", s.handleExportPersonalRecords)
	authGroup.GET("/finance", s.handleFinanceSummary)
	authGroup.GET("/availability", s.handleAvailabilityOverview)
	authGroup.GET("/availability/me", s.handleMyAvailability)
	authGroup.PUT("/availability/me", s.handleSaveAvailability)
	authGroup.GET("/schedule", s.handleSchedule)
	authGroup.GET("/final-schedules/:week", s.handleFinalSchedule)
	authGroup.GET("/work-orders", middleware.RequireRoles("ADMIN", "OWNER", "LEADER", "HR", "FINANCE"), s.handleListWorkOrders)
	authGroup.GET("/work-orders/export", middleware.RequireRoles("ADMIN", "OWNER", "HR", "FINANCE"), s.handleExportWorkOrders)
	authGroup.GET("/finance/export", middleware.RequireRoles("ADMIN", "OWNER", "FINANCE"), s.handleExportFinance)
	authGroup.GET("/finance/duty-csv", middleware.RequireRoles("ADMIN", "OWNER", "FINANCE"), s.handleExportDutyCSV)
	authGroup.POST("/finance/save-local", middleware.RequireRoles("ADMIN", "OWNER", "FINANCE"), s.handleSaveFinanceLocal)

	managerGroup := authGroup.Group("")
	managerGroup.Use(middleware.RequireRoles("ADMIN", "OWNER", "HR"))
	managerGroup.GET("/availability/users/:username", s.handleUserAvailability)
	managerGroup.PUT("/availability/users/:username", s.handleSaveUserAvailability)
	managerGroup.PUT("/schedule", s.handleSaveSchedule)
	managerGroup.GET("/schedule/export", s.handleExportSchedule)
	managerGroup.POST("/schedule/auto-generate", s.handleAutoGenerateSchedule)

	workOrderManagerGroup := authGroup.Group("")
	workOrderManagerGroup.Use(middleware.RequireRoles("ADMIN", "OWNER", "LEADER", "FINANCE"))
	workOrderManagerGroup.POST("/work-orders", s.handleCreateWorkOrder)
	workOrderManagerGroup.PUT("/work-orders/:id", s.handleUpdateWorkOrder)
	workOrderManagerGroup.DELETE("/work-orders/:id", s.handleDeleteWorkOrder)

	adminGroup := authGroup.Group("")
	adminGroup.Use(middleware.RequireRoles("ADMIN"))
	adminGroup.GET("/users", s.handleUsers)
	adminGroup.POST("/users", s.handleCreateUser)
	adminGroup.PATCH("/users/:id/profile", s.handleUpdateUserProfile)
	adminGroup.PATCH("/users/:id/membership", s.handleRestoreUserMembership)
	adminGroup.DELETE("/users/:id/membership", s.handleRemoveUserMembership)
	adminGroup.PATCH("/users/:id/status", s.handleUpdateUserStatus)
	adminGroup.PATCH("/users/:id/password", s.handleResetPassword)
	adminGroup.POST("/labor-convert", s.handleLaborConvert)
	adminGroup.GET("/labor-convert/finance-files", s.handleLaborConvertFinanceFiles)
	adminGroup.DELETE("/labor-convert/finance-files/:id", s.handleDeleteLaborConvertFinanceFile)
	adminGroup.POST("/labor-convert/from-finance", s.handleLaborConvertFromFinance)
	adminGroup.GET("/labor-convert/history", s.handleLaborConvertHistory)
	adminGroup.GET("/labor-convert/history/:id", s.handleLaborConvertHistoryDetail)
	adminGroup.GET("/labor-convert/history/:id/download", s.handleDownloadLaborConvertWorkbook)
	adminGroup.GET("/labor-convert/history/:id/download/work-study", s.handleDownloadLaborWorkStudyConversionWorkbook)
	adminGroup.GET("/labor-convert/history/:id/download/csv", s.handleDownloadLaborConvertCSV)
	adminGroup.GET("/labor-convert/history/:id/download/records", s.handleDownloadLaborConvertRecords)
	adminGroup.POST("/labor-convert/history/:id/manual-adjust", s.handleManualAdjustLaborConvert)
	adminGroup.DELETE("/labor-convert/history/:id", s.handleDeleteLaborConvertHistory)
	adminGroup.GET("/templates/global", s.handleGetTemplateStatus)
	adminGroup.PUT("/templates/global", s.handleUploadTemplate)
	adminGroup.GET("/templates/global/download", s.handleDownloadTemplate)
	adminGroup.DELETE("/templates/global", s.handleDeleteTemplate)

	systemSettingsGroup := authGroup.Group("")
	systemSettingsGroup.Use(middleware.RequireRoles("ADMIN", "OWNER"))
	systemSettingsGroup.GET("/system-settings", s.handleGetSystemSettings)
	systemSettingsGroup.PUT("/system-settings", s.handleUpdateSystemSettings)

	finalScheduleGroup := authGroup.Group("")
	finalScheduleGroup.Use(middleware.RequireRoles("ADMIN", "OWNER", "HR"))
	finalScheduleGroup.PUT("/final-schedules/:week", s.handleSaveFinalSchedule)

	registerFrontendRoutes(router)

	return router
}

func (s *server) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, middleware.CurrentUser(c))
}

func (s *server) handleMetaConfig(c *gin.Context) {
	activeValue, _ := c.Get("active_semester")
	active, _ := activeValue.(types.SemesterSummary)
	c.JSON(http.StatusOK, types.MetaConfigResponse{
		WeekdaysCode:    config.WeekdaysCode,
		WeekdaysDisplay: config.WeekdaysDisplay,
		TimeSlots:       config.TimeSlots,
		UserNames:       config.MemberNames(),
		UserRoles:       config.UserRoles,
		FirstMonday:     active.FirstMonday,
		Semester:        active,
	})
}

func (s *server) handleDashboard(c *gin.Context) {
	user := middleware.CurrentUser(c)
	data, err := s.storeFor(c).GetDashboard(slices.Contains(user.Permissions, "view_workorders"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载首页数据失败"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (s *server) handleFinanceSummary(c *gin.Context) {
	targetUser, err := s.resolveFinanceTarget(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	startDate := strings.TrimSpace(c.Query("startDate"))
	endDate := strings.TrimSpace(c.Query("endDate"))
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择完整的起止日期"})
		return
	}
	workOrderIDs := splitQueryList(c.Query("workOrderIds"))
	includeManagement := strings.EqualFold(strings.TrimSpace(c.Query("includeManagement")), "true")
	managementMonths, err := strconv.Atoi(strings.TrimSpace(c.Query("managementMonths")))
	if err != nil || managementMonths < 0 {
		managementMonths = 0
	}
	data, err := s.storeFor(c).GetFinanceSummaryForRange(startDate, endDate, workOrderIDs, includeManagement, managementMonths, targetUser.RealName, targetUser.Role)
	if err != nil {
		if errors.Is(err, store.ErrInvalidDateRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围格式错误"})
			return
		}
		if errors.Is(err, store.ErrDateRangeTooWide) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围不能超过一年，请缩小范围"})
			return
		}
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期超出允许范围"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载财务统计失败"})
		return
	}

	c.JSON(http.StatusOK, data)
}

func (s *server) resolveFinanceTarget(c *gin.Context) (*types.User, error) {
	currentUser := middleware.CurrentUser(c)
	targetRealName := strings.TrimSpace(c.Query("realName"))

	if targetRealName == "" {
		if currentUser.Role == "ADMIN" || currentUser.Role == "OWNER" || currentUser.Role == "FINANCE" {
			target := currentUser
			target.RealName = ""
			return &target, nil
		}
		return &currentUser, nil
	}

	if currentUser.Role != "ADMIN" && currentUser.Role != "OWNER" && currentUser.Role != "FINANCE" {
		return &currentUser, nil
	}

	targetUser, err := s.storeFor(c).GetUserByRealName(targetRealName)
	if err != nil {
		return nil, fmt.Errorf("指定成员不存在")
	}

	return targetUser, nil
}

func (s *server) handleAvailabilityOverview(c *gin.Context) {
	data, err := s.storeFor(c).GetAvailabilityOverview()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载空闲时间失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": data})
}

func (s *server) handleMyAvailability(c *gin.Context) {
	user := middleware.CurrentUser(c)
	data, err := s.storeFor(c).GetAvailabilityForUser(user.RealName)
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
	if err := s.storeFor(c).SaveAvailability(user.RealName, request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, types.MessageResponse{Message: "空闲时间已保存"})
}

func (s *server) handleUserAvailability(c *gin.Context) {
	user, err := s.storeFor(c).GetUserByUsername(strings.TrimSpace(c.Param("username")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}

	data, err := s.storeFor(c).GetAvailabilityForUser(user.RealName)
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

	user, err := s.storeFor(c).GetUserByUsername(strings.TrimSpace(c.Param("username")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "用户不存在"})
		return
	}

	if err := s.storeFor(c).SaveAvailability(user.RealName, request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, types.MessageResponse{Message: "用户空闲时间已保存"})
}

func (s *server) handleSchedule(c *gin.Context) {
	data, err := s.storeFor(c).GetScheduleSummary()
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

	if err := s.storeFor(c).SaveSchedule(request.Schedule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "排班已保存"})
}

func (s *server) handleAutoGenerateSchedule(c *gin.Context) {
	var request types.AutoScheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "自动排班参数错误"})
		return
	}

	result, err := s.storeFor(c).GenerateAutoSchedule(request.PerSlot, request.Schedule)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
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

	data, err := s.storeFor(c).GetFinalSchedule(weekNumber, selectedDate)
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
	if err := s.storeFor(c).SaveFinalSchedule(weekNumber, request, user.RealName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "实际值班表已保存"})
}

func (s *server) handleListWorkOrders(c *gin.Context) {
	month := c.Query("month")
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	result, err := s.storeFor(c).ListWorkOrdersPage(month, page, pageSize)
	if err != nil {
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "月份超出允许范围"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载工单失败"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleCreateWorkOrder(c *gin.Context) {
	var request types.SaveWorkOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "工单参数错误"})
		return
	}

	user := middleware.CurrentUser(c)
	workOrder, err := s.storeFor(c).CreateWorkOrder(request, user.RealName)
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

	workOrder, err := s.storeFor(c).UpdateWorkOrder(c.Param("id"), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, workOrder)
}

func (s *server) handleDeleteWorkOrder(c *gin.Context) {
	if err := s.storeFor(c).DeleteWorkOrder(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "删除工单失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "工单已删除"})
}

func (s *server) handleExportSchedule(c *gin.Context) {
	content, err := s.storeFor(c).ExportScheduleWorkbook()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出排班失败"})
		return
	}

	c.Header("Content-Disposition", `attachment; filename="schedule.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleExportWorkOrders(c *gin.Context) {
	month := c.Query("month")
	content, err := s.storeFor(c).ExportWorkOrdersWorkbook(month)
	if err != nil {
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "月份超出允许范围"})
			return
		}
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

func (s *server) handleExportFinance(c *gin.Context) {
	startDate := strings.TrimSpace(c.Query("startDate"))
	endDate := strings.TrimSpace(c.Query("endDate"))
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择完整的起止日期"})
		return
	}
	workOrderIDs := splitQueryList(c.Query("workOrderIds"))
	includeManagement := strings.EqualFold(strings.TrimSpace(c.Query("includeManagement")), "true")
	managementMonths, err := strconv.Atoi(strings.TrimSpace(c.Query("managementMonths")))
	if err != nil || managementMonths < 0 {
		managementMonths = 0
	}
	content, err := s.storeFor(c).ExportFinanceWorkbookForRange(startDate, endDate, workOrderIDs, includeManagement, managementMonths)
	if err != nil {
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期超出允许范围"})
			return
		}
		if errors.Is(err, store.ErrInvalidDateRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围格式错误"})
			return
		}
		if errors.Is(err, store.ErrDateRangeTooWide) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围不能超过一年，请缩小范围"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出财务统计失败"})
		return
	}

	filename := fmt.Sprintf("%s-%s-财务统计.xlsx", compactDate(startDate), compactDate(endDate))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleExportDutyCSV(c *gin.Context) {
	startDate := strings.TrimSpace(c.Query("startDate"))
	endDate := strings.TrimSpace(c.Query("endDate"))
	outputMonth := strings.TrimSpace(c.Query("outputMonth"))
	workOrderIDs := splitQueryList(c.Query("workOrderIds"))
	includeManagement := strings.EqualFold(strings.TrimSpace(c.Query("includeManagement")), "true")
	managementMonths, err := strconv.Atoi(strings.TrimSpace(c.Query("managementMonths")))
	if err != nil || managementMonths < 0 {
		managementMonths = 0
	}
	if startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择完整的起止日期"})
		return
	}

	content, err := s.storeFor(c).ExportDutyCSVForRange(startDate, endDate, outputMonth, workOrderIDs, includeManagement, managementMonths)
	if err != nil {
		if errors.Is(err, store.ErrInvalidDateRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围格式错误"})
			return
		}
		if errors.Is(err, store.ErrDateRangeTooWide) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围不能超过一年，请缩小范围"})
			return
		}
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "导出月份超出允许范围"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "导出值班 CSV 失败"})
		return
	}

	filenameMonth := outputMonth
	if filenameMonth == "" {
		filenameMonth = strings.TrimSpace(startDate)
		if len(filenameMonth) >= len("2006-01") {
			filenameMonth = filenameMonth[:len("2006-01")]
		}
	}
	filename := fmt.Sprintf("%s-%s-%s-duty_by_person.csv", compactDate(startDate), compactDate(endDate), strings.ReplaceAll(filenameMonth, "-", ""))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", content)
}

func (s *server) handleSaveFinanceLocal(c *gin.Context) {
	var request types.FinanceSaveLocalRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "保存参数格式错误"})
		return
	}
	if strings.TrimSpace(request.StartDate) == "" || strings.TrimSpace(request.EndDate) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择完整的起止日期"})
		return
	}

	response, err := s.storeFor(c).SaveFinanceExportsLocal(request)
	if err != nil {
		if errors.Is(err, store.ErrInvalidDateRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围格式错误"})
			return
		}
		if errors.Is(err, store.ErrDateRangeTooWide) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "日期范围不能超过一年，请缩小范围"})
			return
		}
		if errors.Is(err, store.ErrMonthOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "导出月份超出允许范围"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "保存财务 Excel 和 CSV 失败"})
		return
	}
	c.JSON(http.StatusOK, response)
}

func splitQueryList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func compactDate(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "-", "")
}

func (s *server) handleUsers(c *gin.Context) {
	users, err := s.storeFor(c).ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载用户失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": users})
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

	if err := s.storeFor(c).UpdateUserStatus(userID, request.IsActive); err != nil {
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

	if err := s.storeFor(c).ResetPassword(userID, request.NewPassword); err != nil {
		if errors.Is(err, config.ErrWeakPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "重置密码失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "密码已重置，该成员所有登录状态已失效，下次登录将强制修改"})
}
