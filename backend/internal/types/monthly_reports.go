package types

type MonthlyConditions struct {
	OutputMonth       string   `json:"outputMonth"`
	StartDate         string   `json:"startDate"`
	EndDate           string   `json:"endDate"`
	WorkOrderIDs      []string `json:"workOrderIds"`
	IncludeManagement bool     `json:"includeManagement"`
	ManagementMonths  int      `json:"managementMonths"`
}

type MonthlyReportRequest struct {
	MonthlyConditions
	TargetTotal    string   `json:"targetTotal"`
	ProxyMemberIDs *[]int64 `json:"proxyMemberIds"`
	SourceHash     string   `json:"sourceHash"`
}

type MonthlyAmountRow struct {
	MemberID int64  `json:"memberId"`
	Amount   string `json:"amount"`
}

type MonthlyVersionRequest struct {
	BaseVersionID      string             `json:"baseVersionId"`
	Mode               string             `json:"mode"`
	TargetTotal        string             `json:"targetTotal"`
	ProxyMemberIDs     *[]int64           `json:"proxyMemberIds"`
	Rows               []MonthlyAmountRow `json:"rows"`
	Conditions         *MonthlyConditions `json:"conditions"`
	ConfirmHighAmounts bool               `json:"confirmHighAmounts"`
}

type MonthlyFinalizeRequest struct {
	VersionID          string `json:"versionId"`
	ConfirmHighAmounts bool   `json:"confirmHighAmounts"`
}

type MonthlyReportSummary struct {
	ID              string `json:"id"`
	Month           string `json:"month"`
	FinalVersionID  string `json:"finalVersionId"`
	LatestVersionID string `json:"latestVersionId"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type MonthlyVersionSummary struct {
	ID            string `json:"id"`
	Number        int    `json:"number"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	TargetTotal   string `json:"targetTotal"`
	OriginalTotal string `json:"originalTotal"`
	DeclaredTotal string `json:"declaredTotal"`
	LegacyNote    string `json:"legacyNote,omitempty"`
}

type MonthlyRow struct {
	MemberID          int64   `json:"memberId"`
	Name              string  `json:"name"`
	StudentNumber     string  `json:"studentNumber"`
	Role              string  `json:"role"`
	DutyHours         float64 `json:"dutyHours"`
	WorkHours         float64 `json:"workHours"`
	DutyAmount        string  `json:"dutyAmount"`
	WorkAmount        string  `json:"workAmount"`
	Management        string  `json:"management"`
	Original          string  `json:"original"`
	Declared          string  `json:"declared"`
	Eligible          bool    `json:"eligible"`
	SalaryOutgoing    string  `json:"salaryOutgoing"`
	SalaryIncoming    string  `json:"salaryIncoming"`
	SalaryOutstanding string  `json:"salaryOutstanding"`
	TeamFund          string  `json:"teamFund"`
}

type MonthlyTransferRow struct {
	SourceName   string `json:"sourceName"`
	ReceiverName string `json:"receiverName"`
	Kind         string `json:"kind"`
	Amount       string `json:"amount"`
}

type MonthlyReportView struct {
	Report        MonthlyReportSummary    `json:"report"`
	Version       MonthlyVersionSummary   `json:"version"`
	Versions      []MonthlyVersionSummary `json:"versions"`
	Conditions    MonthlyConditions       `json:"conditions"`
	Rows          []MonthlyRow            `json:"rows"`
	Transfers     []MonthlyTransferRow    `json:"transfers"`
	OriginalTotal string                  `json:"originalTotal"`
	TargetTotal   string                  `json:"targetTotal"`
	DeclaredTotal string                  `json:"declaredTotal"`
	TeamFund      string                  `json:"teamFund"`
	Unallocated   string                  `json:"unallocated"`
	SourceHash    string                  `json:"sourceHash"`
	Issues        []string                `json:"issues"`
	Notes         []string                `json:"notes"`
	Files         []string                `json:"files"`
	TemplateHash  string                  `json:"templateHash"`
	Sources       []MonthlySourceSummary  `json:"sources"`
}

type MonthlySourceSummary struct {
	ID        string `json:"id"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	CreatedAt string `json:"createdAt"`
	Legacy    bool   `json:"legacy"`
	HasExcel  bool   `json:"hasExcel"`
	HasCSV    bool   `json:"hasCsv"`
}
