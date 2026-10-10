package store

import "personnel-management-go/internal/types"

const monthlyStep int64 = 2500
const monthlyAutoCap int64 = 190000
const monthlyHardCap int64 = 195000

// MonthlySnapshot is the immutable accounting input, independent of current
// membership, settings and work orders. Monetary values are integer cents.
type MonthlySnapshot struct {
	Conditions        types.MonthlyConditions `json:"conditions"`
	FirstMonday       string                  `json:"firstMonday"`
	PublishedPlanID   string                  `json:"publishedPlanId"`
	PublishedPlanName string                  `json:"publishedPlanName"`
	WorkContent       string                  `json:"workContent"`
	Rates             RateConfig              `json:"rates"`
	Members           []MonthlyMember         `json:"members"`
	Duty              []MonthlyDuty           `json:"duty"`
	WorkOrders        []types.WorkOrder       `json:"workOrders"`
}

type MonthlyMember struct {
	ID            int64   `json:"id"`
	AccountUUID   string  `json:"accountUuid"`
	Name          string  `json:"name"`
	StudentNumber string  `json:"studentNumber"`
	Role          string  `json:"role"`
	SortOrder     int     `json:"sortOrder"`
	DutyHours     float64 `json:"dutyHours"`
	WorkHours     float64 `json:"workHours"`
	DutyAmount    int64   `json:"dutyAmount"`
	WorkAmount    int64   `json:"workAmount"`
	Management    int64   `json:"management"`
	Original      int64   `json:"original"`
}

type MonthlyDuty struct {
	MemberID int64   `json:"memberId"`
	Date     string  `json:"date"`
	Start    string  `json:"start"`
	End      string  `json:"end"`
	Hours    float64 `json:"hours"`
	Source   string  `json:"source"`
}

type MonthlyAllocatedMember struct {
	MonthlyMember
	Eligible          bool  `json:"eligible"`
	Declared          int64 `json:"declared"`
	SalaryOutgoing    int64 `json:"salaryOutgoing"`
	SalaryIncoming    int64 `json:"salaryIncoming"`
	SalaryOutstanding int64 `json:"salaryOutstanding"`
	TeamFund          int64 `json:"teamFund"`
}

type MonthlyTransfer struct {
	SourceID     int64  `json:"sourceId"`
	SourceName   string `json:"sourceName"`
	ReceiverID   int64  `json:"receiverId"`
	ReceiverName string `json:"receiverName"`
	Kind         string `json:"kind"`
	Amount       int64  `json:"amount"`
}

type MonthlyDistribution struct {
	Rows        []MonthlyAllocatedMember `json:"rows"`
	Transfers   []MonthlyTransfer        `json:"transfers"`
	Target      int64                    `json:"target"`
	Original    int64                    `json:"original"`
	Declared    int64                    `json:"declared"`
	TeamFund    int64                    `json:"teamFund"`
	Unallocated int64                    `json:"unallocated"`
}

// AllocateMonthlyReport follows the administrator's frozen member order.
// It returns an incomplete draft when the chosen proxy capacity is insufficient.
func AllocateMonthlyReport(snapshot MonthlySnapshot, target int64, eligible []int64) (MonthlyDistribution, error) {
	amounts := make(map[int64]int64, len(snapshot.Members))
	allowed := make(map[int64]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	var baseline int64
	for _, member := range snapshot.Members {
		base := min(member.Original, monthlyAutoCap) / monthlyStep * monthlyStep
		amounts[member.ID] = base
		baseline += base
	}
	remaining := target - baseline
	for _, stage := range []struct {
		zero bool
		cap  int64
	}{{true, 80000}, {true, 160000}, {false, monthlyAutoCap}} {
		for _, member := range snapshot.Members {
			if !allowed[member.ID] || (member.Original == 0) != stage.zero {
				continue
			}
			for remaining >= monthlyStep && amounts[member.ID] < stage.cap {
				step := min(int64(5000), remaining, stage.cap-amounts[member.ID]) / monthlyStep * monthlyStep
				if step == 0 {
					break
				}
				amounts[member.ID] += step
				remaining -= step
			}
		}
	}
	return buildMonthlyDistribution(snapshot, target, amounts, allowed)
}

// AdjustMonthlyReport creates a new distribution without altering earned pay.
func AdjustMonthlyReport(snapshot MonthlySnapshot, amounts map[int64]int64, eligible []int64) (MonthlyDistribution, error) {
	allowed := make(map[int64]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	if len(amounts) != len(snapshot.Members) {
		return MonthlyDistribution{}, monthlyInputErrorf("调额名单必须与来源快照一致")
	}
	var target int64
	for _, member := range snapshot.Members {
		amount, found := amounts[member.ID]
		if !found {
			return MonthlyDistribution{}, monthlyInputErrorf("缺少 %s 的申报金额", member.Name)
		}
		target += amount
	}
	return buildMonthlyDistribution(snapshot, target, amounts, allowed)
}

func buildMonthlyDistribution(snapshot MonthlySnapshot, target int64, amounts map[int64]int64, allowed map[int64]bool) (MonthlyDistribution, error) {
	result := MonthlyDistribution{Rows: []MonthlyAllocatedMember{}, Transfers: []MonthlyTransfer{}, Target: target}
	if target <= 0 {
		return result, monthlyInputErrorf("目标总额必须为正数")
	}
	seen := make(map[int64]bool, len(snapshot.Members))
	for _, member := range snapshot.Members {
		if member.Original < 0 || seen[member.ID] {
			return result, monthlyInputErrorf("来源成员或应发金额无效")
		}
		seen[member.ID] = true
		declared := amounts[member.ID]
		if declared < 0 || declared > monthlyHardCap || declared%monthlyStep != 0 {
			return result, monthlyInputErrorf("%s 的申报额必须为25元整数倍且不超过1950元", member.Name)
		}
		if declared > member.Original && !allowed[member.ID] {
			return result, monthlyInputErrorf("%s 未勾选参与代发，不能接收额外金额", member.Name)
		}
		outgoing := max(int64(0), member.Original-declared)
		result.Rows = append(result.Rows, MonthlyAllocatedMember{MonthlyMember: member, Eligible: allowed[member.ID], Declared: declared, SalaryOutgoing: outgoing, SalaryOutstanding: outgoing})
		result.Original += member.Original
		result.Declared += declared
	}
	for id := range allowed {
		if !seen[id] {
			return result, monthlyInputErrorf("代发名单包含快照外的成员")
		}
	}
	if target < result.Original {
		return result, monthlyInputErrorf("目标总额不能低于实际应发合计")
	}
	result.TeamFund = target - result.Original
	result.Unallocated = target - result.Declared
	if result.Unallocated < 0 {
		return result, monthlyInputErrorf("个人申报合计超过目标总额")
	}
	for receiverIndex := range result.Rows {
		receiver := &result.Rows[receiverIndex]
		capacity := max(int64(0), receiver.Declared-receiver.Original)
		for sourceIndex := range result.Rows {
			if capacity == 0 {
				break
			}
			source := &result.Rows[sourceIndex]
			amount := min(capacity, source.SalaryOutstanding)
			if amount == 0 {
				continue
			}
			source.SalaryOutstanding -= amount
			receiver.SalaryIncoming += amount
			capacity -= amount
			result.Transfers = append(result.Transfers, MonthlyTransfer{SourceID: source.ID, SourceName: source.Name, ReceiverID: receiver.ID, ReceiverName: receiver.Name, Kind: "salary", Amount: amount})
		}
		if capacity > 0 {
			receiver.TeamFund = capacity
			result.Transfers = append(result.Transfers, MonthlyTransfer{SourceName: "团队经费", ReceiverID: receiver.ID, ReceiverName: receiver.Name, Kind: "team_fund", Amount: capacity})
		}
	}
	return result, nil
}
