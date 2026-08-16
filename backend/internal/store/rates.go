package store

import (
	"fmt"
	"time"
)

// RateConfig holds the semester-level payout rates used by finance summaries,
// exports, and labor conversion. All money values are stored in cents.
type RateConfig struct {
	DutyCents       int64 // 值班时薪，默认 2500（25 元/小时）
	WorkOrderCents  int64 // 工单时薪，默认 5000（50 元/小时）
	MgmtLeaderCents int64 // 组长/人事专员项目管理月薪，默认 80000（800 元/月）
	MgmtOwnerCents  int64 // 负责人项目管理月薪，默认 120000（1200 元/月）
}

func DefaultRateConfig() RateConfig {
	return RateConfig{
		DutyCents:       2500,
		WorkOrderCents:  5000,
		MgmtLeaderCents: 80000,
		MgmtOwnerCents:  120000,
	}
}

const rateMaxCents int64 = 1000000 // 单项费率上限 1 万元

// normalized replaces unset (zero) values with the defaults. It is only used
// on initialization paths; explicit updates go through validate, which accepts
// a zero management rate.
func (r RateConfig) normalized() RateConfig {
	defaults := DefaultRateConfig()
	if r.DutyCents <= 0 {
		r.DutyCents = defaults.DutyCents
	}
	if r.WorkOrderCents <= 0 {
		r.WorkOrderCents = defaults.WorkOrderCents
	}
	if r.MgmtLeaderCents == 0 {
		r.MgmtLeaderCents = defaults.MgmtLeaderCents
	}
	if r.MgmtOwnerCents == 0 {
		r.MgmtOwnerCents = defaults.MgmtOwnerCents
	}
	return r
}

func (r RateConfig) validate() error {
	if r.DutyCents <= 0 || r.DutyCents > rateMaxCents {
		return fmt.Errorf("值班时薪必须是 0.01 至 10000 元之间的数值")
	}
	if r.WorkOrderCents <= 0 || r.WorkOrderCents > rateMaxCents {
		return fmt.Errorf("工单时薪必须是 0.01 至 10000 元之间的数值")
	}
	if r.MgmtLeaderCents < 0 || r.MgmtLeaderCents > rateMaxCents {
		return fmt.Errorf("组长/人事项目管理薪必须是 0 至 10000 元之间的数值")
	}
	if r.MgmtOwnerCents < 0 || r.MgmtOwnerCents > rateMaxCents {
		return fmt.Errorf("负责人项目管理薪必须是 0 至 10000 元之间的数值")
	}
	return nil
}

func (r RateConfig) DutyYuan() float64 {
	return float64(r.DutyCents) / 100
}

func (r RateConfig) WorkOrderYuan() float64 {
	return float64(r.WorkOrderCents) / 100
}

func (r RateConfig) mgmtCentsForRole(role string) int64 {
	switch role {
	case "LEADER", "HR":
		return r.MgmtLeaderCents
	case "OWNER":
		return r.MgmtOwnerCents
	default:
		return 0
	}
}

func (s *Store) calculateManagementAmount(month string, role string, now time.Time) (float64, bool) {
	if isFutureMonth(month, now) {
		return 0, true
	}
	return float64(s.rates.mgmtCentsForRole(role)) / 100, false
}

func (s *Store) calculateManagementAmountForMonthCount(role string, months int) float64 {
	if months <= 0 {
		return 0
	}
	return float64(months) * float64(s.rates.mgmtCentsForRole(role)) / 100
}
