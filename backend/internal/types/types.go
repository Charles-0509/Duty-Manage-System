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
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
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
}

type MessageResponse struct {
	Message string `json:"message"`
}

type SystemSettingsResponse struct {
	AppPort            string `json:"appPort"`
	DatabasePath       string `json:"databasePath"`
	PrivateMembersPath string `json:"privateMembersPath"`
	FirstMonday        string `json:"firstMonday"`
	EnvFilePath        string `json:"envFilePath"`
}

type UpdateSystemSettingsRequest struct {
	DatabasePath       string `json:"databasePath"`
	PrivateMembersPath string `json:"privateMembersPath"`
	FirstMonday        string `json:"firstMonday"`
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
