package types

type User struct {
	ID                 int64    `json:"id"`
	Username           string   `json:"username"`
	RealName           string   `json:"realName"`
	StudentNumber      string   `json:"studentNumber"`
	Role               string   `json:"role"`
	IsActive           bool     `json:"isActive"`
	MustChangePassword bool     `json:"mustChangePassword"`
	CreatedAt          string   `json:"createdAt,omitempty"`
	Permissions        []string `json:"permissions"`
	SemesterMember     bool     `json:"semesterMember"`
	SortOrder          int      `json:"sortOrder"`
	// SessionVersion is the per-account token generation. Access tokens whose
	// SessionVersion is older are rejected. Never serialized to clients.
	SessionVersion int64 `json:"-"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	User         User   `json:"user"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type ChangePasswordResponse struct {
	Message      string `json:"message"`
	User         User   `json:"user"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
}

type AdminResetPasswordRequest struct {
	NewPassword string `json:"newPassword"`
}

type UpdateUserStatusRequest struct {
	IsActive bool `json:"isActive"`
}

type AvailabilityPayload struct {
	Single []string `json:"single"`
	Double []string `json:"double"`
}

type AvailabilityOverviewItem struct {
	Username     string              `json:"username"`
	RealName     string              `json:"realName"`
	Availability AvailabilityPayload `json:"availability"`
}

type SaveAvailabilityRequest struct {
	Single []string `json:"single"`
	Double []string `json:"double"`
}

type ScheduleResponse struct {
	Schedule          map[string][]string `json:"schedule"`
	ShiftDistribution []ChartItem         `json:"shiftDistribution"`
}

type SchedulePlanSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IsPublished bool   `json:"isPublished"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type SchedulePlanResponse struct {
	Plan              SchedulePlanSummary `json:"plan"`
	Schedule          map[string][]string `json:"schedule"`
	ShiftDistribution []ChartItem         `json:"shiftDistribution"`
}

type SaveSchedulePlanRequest struct {
	Name     string              `json:"name"`
	Schedule map[string][]string `json:"schedule"`
}

type RenameSchedulePlanRequest struct {
	Name string `json:"name"`
}

type FinalScheduleResponse struct {
	WeekNumber   int                 `json:"weekNumber"`
	SelectedDate string              `json:"selectedDate"`
	IsOddWeek    bool                `json:"isOddWeek"`
	Source       string              `json:"source"`
	Schedule     map[string][]string `json:"schedule"`
}

type SaveFinalScheduleRequest struct {
	SelectedDate string              `json:"selectedDate"`
	Schedule     map[string][]string `json:"schedule"`
}

type WorkSession struct {
	ID         int64   `json:"id,omitempty"`
	Date       string  `json:"date"`
	WorkerName string  `json:"workerName"`
	Duration   float64 `json:"duration"`
}

type WorkOrder struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	BelongingMonth string        `json:"belongingMonth"`
	CreatedTime    string        `json:"createdTime"`
	CreatedBy      string        `json:"createdBy"`
	WorkSessions   []WorkSession `json:"workSessions"`
}

type WorkOrderListResponse struct {
	Items    []WorkOrder `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

type SaveWorkOrderRequest struct {
	Title          string        `json:"title"`
	BelongingMonth string        `json:"belongingMonth"`
	WorkSessions   []WorkSession `json:"workSessions"`
}

type ChartItem struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type DashboardResponse struct {
	AvailabilityUserCount int                 `json:"availabilityUserCount"`
	TotalAssignedShifts   int                 `json:"totalAssignedShifts"`
	WorkOrderCount        int                 `json:"workOrderCount"`
	Schedule              map[string][]string `json:"schedule"`
	ShiftDistribution     []ChartItem         `json:"shiftDistribution"`
	WorkDurationShare     []ChartItem         `json:"workDurationShare"`
}

type FinanceWorkOrderDetail struct {
	Title  string  `json:"title"`
	Dates  string  `json:"dates"`
	Hours  float64 `json:"hours"`
	Amount float64 `json:"amount"`
}

type FinanceSummaryResponse struct {
	Month             string                   `json:"month"`
	StartDate         string                   `json:"startDate,omitempty"`
	EndDate           string                   `json:"endDate,omitempty"`
	DutyHours         float64                  `json:"dutyHours"`
	DutyAmount        float64                  `json:"dutyAmount"`
	WorkOrderHours    float64                  `json:"workOrderHours"`
	WorkOrderAmount   float64                  `json:"workOrderAmount"`
	ManagementAmount  float64                  `json:"managementAmount"`
	ManagementPending bool                     `json:"managementPending"`
	TotalAmount       float64                  `json:"totalAmount"`
	WorkOrderDetails  []FinanceWorkOrderDetail `json:"workOrderDetails"`
	MemberSummaries   []FinanceMemberSummary   `json:"memberSummaries,omitempty"`
}

type FinanceMemberSummary struct {
	RealName         string  `json:"realName"`
	DutyAmount       float64 `json:"dutyAmount"`
	WorkOrderAmount  float64 `json:"workOrderAmount"`
	ManagementAmount float64 `json:"managementAmount"`
	TotalAmount      float64 `json:"totalAmount"`
}

type MetaConfigResponse struct {
	WeekdaysCode    []string          `json:"weekdaysCode"`
	WeekdaysDisplay []string          `json:"weekdaysDisplay"`
	TimeSlots       []string          `json:"timeSlots"`
	UserNames       []string          `json:"userNames"`
	UserRoles       map[string]string `json:"userRoles"`
	FirstMonday     string            `json:"firstMonday"`
	Semester        SemesterSummary   `json:"semester"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type SystemSettingsResponse struct {
	FirstMonday      string          `json:"firstMonday"`
	WorkStudyContent string          `json:"workStudyContent"`
	Semester         SemesterSummary `json:"semester"`
	DutyRate         float64         `json:"dutyRate"`
	WorkOrderRate    float64         `json:"workOrderRate"`
	MgmtLeaderRate   float64         `json:"mgmtLeaderRate"`
	MgmtOwnerRate    float64         `json:"mgmtOwnerRate"`
}

type UpdateSystemSettingsRequest struct {
	FirstMonday      string  `json:"firstMonday"`
	WorkStudyContent string  `json:"workStudyContent"`
	DutyRate         float64 `json:"dutyRate"`
	WorkOrderRate    float64 `json:"workOrderRate"`
	MgmtLeaderRate   float64 `json:"mgmtLeaderRate"`
	MgmtOwnerRate    float64 `json:"mgmtOwnerRate"`
}

type AuditLogEntry struct {
	Username   string `json:"username"`
	RealName   string `json:"realName"`
	Action     string `json:"action"`
	Status     int    `json:"status"`
	SemesterID string `json:"semesterId"`
	IP         string `json:"ip"`
}

type AuditLogItem struct {
	ID         int64  `json:"id"`
	CreatedAt  string `json:"createdAt"`
	Username   string `json:"username"`
	RealName   string `json:"realName"`
	Action     string `json:"action"`
	Status     int    `json:"status"`
	SemesterID string `json:"semesterId"`
	IP         string `json:"ip"`
}

type AuditLogListResponse struct {
	Items    []AuditLogItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type AutoScheduleRequest struct {
	PerSlot  int                 `json:"perSlot"`
	Schedule map[string][]string `json:"schedule"`
}

type AutoScheduleResponse struct {
	Schedule          map[string][]string `json:"schedule"`
	ShiftDistribution []ChartItem         `json:"shiftDistribution"`
	Warnings          []string            `json:"warnings"`
}

type SemesterSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Database       string `json:"-"`
	Archived       bool   `json:"archived"`
	Draft          bool   `json:"draft"`
	Active         bool   `json:"active"`
	FirstMonday    string `json:"firstMonday"`
	ContextVersion int64  `json:"contextVersion"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type CreateSemesterRequest struct {
	Name        string `json:"name"`
	FirstMonday string `json:"firstMonday"`
	CloneFromID string `json:"cloneFromId"`
}

type UpdateSemesterRequest struct {
	Name string `json:"name"`
}

type CreateMemberRequest struct {
	Username        string `json:"username"`
	RealName        string `json:"realName"`
	StudentNumber   string `json:"studentNumber"`
	Role            string `json:"role"`
	InitialPassword string `json:"initialPassword"`
}

type UpdateMemberRequest struct {
	RealName      string  `json:"realName"`
	StudentNumber *string `json:"studentNumber,omitempty"`
	Role          string  `json:"role"`
	SortOrder     *int    `json:"sortOrder,omitempty"`
}

type WorkStudyTemplateItem struct {
	Filename string `json:"filename"`
	Exists   bool   `json:"exists"`
	Size     int64  `json:"size"`
	Updated  string `json:"updatedAt,omitempty"`
}
