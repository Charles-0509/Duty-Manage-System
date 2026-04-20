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
	DutyHours         float64                  `json:"dutyHours"`
	DutyAmount        float64                  `json:"dutyAmount"`
	WorkOrderHours    float64                  `json:"workOrderHours"`
	WorkOrderAmount   float64                  `json:"workOrderAmount"`
	ManagementAmount  float64                  `json:"managementAmount"`
	ManagementPending bool                     `json:"managementPending"`
	TotalAmount       float64                  `json:"totalAmount"`
	WorkOrderDetails  []FinanceWorkOrderDetail `json:"workOrderDetails"`
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
