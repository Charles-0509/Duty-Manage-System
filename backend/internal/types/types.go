package types

type User struct {
	ID                 int64    `json:"id"`
	Username           string   `json:"username"`
	RealName           string   `json:"realName"`
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

type UpdateRoleRequest struct {
	Role string `json:"role"`
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

type SaveScheduleRequest struct {
	Schedule map[string][]string `json:"schedule"`
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
}

type FinanceSaveLocalRequest struct {
	StartDate         string   `json:"startDate"`
	EndDate           string   `json:"endDate"`
	OutputMonth       string   `json:"outputMonth"`
	WorkOrderIDs      []string `json:"workOrderIds"`
	IncludeManagement bool     `json:"includeManagement"`
	ManagementMonths  int      `json:"managementMonths"`
}

type FinanceLocalBatch struct {
	ID                string   `json:"id"`
	CreatedAt         string   `json:"createdAt"`
	StartDate         string   `json:"startDate"`
	EndDate           string   `json:"endDate"`
	OutputMonth       string   `json:"outputMonth"`
	WorkOrderIDs      []string `json:"workOrderIds"`
	IncludeManagement bool     `json:"includeManagement"`
	ManagementMonths  int      `json:"managementMonths"`
	ExcelFilename     string   `json:"excelFilename"`
	CSVFilename       string   `json:"csvFilename"`
	RelativeDir       string   `json:"relativeDir"`
}

type FinanceSaveLocalResponse struct {
	Message string            `json:"message"`
	Batch   FinanceLocalBatch `json:"batch"`
}

type MetaConfigResponse struct {
	WeekdaysCode    []string            `json:"weekdaysCode"`
	WeekdaysDisplay []string            `json:"weekdaysDisplay"`
	TimeSlots       []string            `json:"timeSlots"`
	UserNames       []string            `json:"userNames"`
	UserRoles       map[string]string   `json:"userRoles"`
	RolePermissions map[string][]string `json:"rolePermissions"`
	FirstMonday     string              `json:"firstMonday"`
	Semester        SemesterSummary     `json:"semester"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

type SystemSettingsResponse struct {
	AppPort          string          `json:"appPort"`
	FirstMonday      string          `json:"firstMonday"`
	LaborSeed        string          `json:"laborSeed"`
	WorkStudyContent string          `json:"workStudyContent"`
	EnvFilePath      string          `json:"envFilePath"`
	Semester         SemesterSummary `json:"semester"`
	DutyRate         float64         `json:"dutyRate"`
	WorkOrderRate    float64         `json:"workOrderRate"`
	MgmtLeaderRate   float64         `json:"mgmtLeaderRate"`
	MgmtOwnerRate    float64         `json:"mgmtOwnerRate"`
}

type UpdateSystemSettingsRequest struct {
	FirstMonday      string  `json:"firstMonday"`
	LaborSeed        string  `json:"laborSeed"`
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
	PerSlot int `json:"perSlot"`
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
	Role            string `json:"role"`
	InitialPassword string `json:"initialPassword"`
}

type UpdateMemberRequest struct {
	RealName  string `json:"realName"`
	Role      string `json:"role"`
	SortOrder *int   `json:"sortOrder,omitempty"`
}

type WorkStudyTemplateItem struct {
	RealName string `json:"realName"`
	Filename string `json:"filename"`
	Exists   bool   `json:"exists"`
	Size     int64  `json:"size"`
	Updated  string `json:"updatedAt,omitempty"`
}

type LaborConvertSummary struct {
	OriginalTotal string            `json:"originalTotal"`
	TargetTotal   string            `json:"targetTotal"`
	FinalTotal    string            `json:"finalTotal"`
	TeamFund      string            `json:"teamFund"`
	Warnings      []string          `json:"warnings"`
	Noise         LaborConvertNoise `json:"noise"`
}

type LaborConvertNoise struct {
	Applied bool                    `json:"applied"`
	Items   []LaborConvertNoiseItem `json:"items"`
}

type LaborConvertNoiseItem struct {
	Name      string `json:"name"`
	Reduction string `json:"reduction"`
}

type LaborConvertRow struct {
	Name     string `json:"name"`
	Original string `json:"original"`
	Adjusted string `json:"adjusted"`
	Delta    string `json:"delta"`
	Tax      string `json:"tax"`
	Net      string `json:"net"`
	Remark   string `json:"remark"`
}

type LaborConvertTransfer struct {
	Source   string `json:"source"`
	Receiver string `json:"receiver"`
	Amount   string `json:"amount"`
}

type LaborConvertResponse struct {
	HistoryID            string                 `json:"historyId"`
	CreatedAt            string                 `json:"createdAt"`
	InputFilename        string                 `json:"inputFilename"`
	OutputName           string                 `json:"outputName"`
	DownloadURL          string                 `json:"downloadUrl"`
	CSVName              string                 `json:"csvName,omitempty"`
	CSVDownloadURL       string                 `json:"csvDownloadUrl,omitempty"`
	HasCSV               bool                   `json:"hasCsv"`
	CSVOutputMonth       string                 `json:"csvOutputMonth,omitempty"`
	SourceFinanceBatchID string                 `json:"sourceFinanceBatchId,omitempty"`
	ParentRunID          string                 `json:"parentRunId,omitempty"`
	IsManualAdjust       bool                   `json:"isManualAdjust"`
	CanManualAdjust      bool                   `json:"canManualAdjust"`
	Seed                 *int64                 `json:"seed,omitempty"`
	Summary              LaborConvertSummary    `json:"summary"`
	Rows                 []LaborConvertRow      `json:"rows"`
	Transfers            []LaborConvertTransfer `json:"transfers"`
}

type LaborConvertHistoryItem struct {
	ID                   string `json:"id"`
	CreatedAt            string `json:"createdAt"`
	InputFilename        string `json:"inputFilename"`
	OutputName           string `json:"outputName"`
	CSVName              string `json:"csvName,omitempty"`
	CSVOutputMonth       string `json:"csvOutputMonth,omitempty"`
	TargetTotal          string `json:"targetTotal"`
	FinalTotal           string `json:"finalTotal"`
	DownloadURL          string `json:"downloadUrl"`
	CSVDownloadURL       string `json:"csvDownloadUrl,omitempty"`
	HasCSV               bool   `json:"hasCsv"`
	CanManualAdjust      bool   `json:"canManualAdjust"`
	SourceFinanceBatchID string `json:"sourceFinanceBatchId,omitempty"`
	IsManualAdjust       bool   `json:"isManualAdjust"`
}

type LaborFinanceFileItem struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"createdAt"`
	StartDate     string `json:"startDate"`
	EndDate       string `json:"endDate"`
	OutputMonth   string `json:"outputMonth"`
	ExcelFilename string `json:"excelFilename"`
	RelativeDir   string `json:"relativeDir"`
}

type LaborConvertFromFinanceRequest struct {
	BatchID     string `json:"batchId"`
	TargetTotal string `json:"targetTotal"`
	Seed        string `json:"seed,omitempty"`
}

type LaborManualAdjustRow struct {
	Name     string `json:"name"`
	Adjusted string `json:"adjusted"`
}

type LaborManualAdjustRequest struct {
	Rows []LaborManualAdjustRow `json:"rows"`
}
