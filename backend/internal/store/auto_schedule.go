package store

import (
	"fmt"
	"sort"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"
)

const (
	autoScheduleMinPerSlot = 1
	autoScheduleMaxPerSlot = 3
)

type autoScheduleMember struct {
	Name   string
	Single map[string]struct{}
	Double map[string]struct{}
	Order  int
	// Load counts assigned (slot, parity) units; every unit weighs 0.5 so a
	// member covering both parities of one slot accumulates 1.0.
	Load float64
}

type autoScheduleUnit struct {
	ShiftCode string
	OddWeek   bool
	Candidate []int
	Taken     map[int]struct{}
}

// GenerateAutoSchedule builds a balanced planned schedule from the registered
// availability. Coverage is planned per parity (单周/双周): every shift slot
// needs perSlot members in odd weeks and perSlot members in even weeks; a
// member available in both parities is stored as one "(单双)" entry. The result
// is a proposal only — callers still save it through SaveSchedule, so manual
// adjustments remain possible.
func (s *Store) GenerateAutoSchedule(perSlot int) (types.AutoScheduleResponse, error) {
	if perSlot < autoScheduleMinPerSlot || perSlot > autoScheduleMaxPerSlot {
		return types.AutoScheduleResponse{}, fmt.Errorf("每班人数必须是 %d 到 %d 之间的整数", autoScheduleMinPerSlot, autoScheduleMaxPerSlot)
	}

	overview, err := s.GetAvailabilityOverview()
	if err != nil {
		return types.AutoScheduleResponse{}, err
	}

	members := make([]*autoScheduleMember, 0, len(overview))
	for index, item := range overview {
		member := &autoScheduleMember{
			Name:   item.RealName,
			Single: map[string]struct{}{},
			Double: map[string]struct{}{},
			Order:  index,
		}
		for _, code := range item.Availability.Single {
			member.Single[code] = struct{}{}
		}
		for _, code := range item.Availability.Double {
			member.Double[code] = struct{}{}
		}
		if len(member.Single) > 0 || len(member.Double) > 0 {
			members = append(members, member)
		}
	}
	if len(members) == 0 {
		return types.AutoScheduleResponse{}, fmt.Errorf("还没有成员登记空闲时间，无法自动排班")
	}

	units := make([]*autoScheduleUnit, 0, len(config.WeekdaysCode)*len(config.TimeSlots)*2)
	for _, dayCode := range config.WeekdaysCode {
		for shiftIndex := range config.TimeSlots {
			code := fmt.Sprintf("%s-%d", dayCode, shiftIndex+1)
			oddUnit := &autoScheduleUnit{ShiftCode: code, OddWeek: true, Taken: map[int]struct{}{}}
			evenUnit := &autoScheduleUnit{ShiftCode: code, OddWeek: false, Taken: map[int]struct{}{}}
			for index, member := range members {
				if _, ok := member.Single[code]; ok {
					oddUnit.Candidate = append(oddUnit.Candidate, index)
				}
				if _, ok := member.Double[code]; ok {
					evenUnit.Candidate = append(evenUnit.Candidate, index)
				}
			}
			units = append(units, oddUnit, evenUnit)
		}
	}

	// Scarcest units first so hard-to-staff slots claim their candidates before
	// popular slots take them.
	sort.SliceStable(units, func(i, j int) bool { return len(units[i].Candidate) < len(units[j].Candidate) })

	warnings := make([]string, 0)
	weekLabel := map[bool]string{true: "单周", false: "双周"}
	for _, unit := range units {
		if len(unit.Candidate) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s %s无人登记空闲时间", unit.ShiftCode, weekLabel[unit.OddWeek]))
			continue
		}
		if len(unit.Candidate) < perSlot {
			warnings = append(warnings, fmt.Sprintf("%s %s仅 %d 人可排，少于每班 %d 人", unit.ShiftCode, weekLabel[unit.OddWeek], len(unit.Candidate), perSlot))
		}

		candidates := make([]int, len(unit.Candidate))
		copy(candidates, unit.Candidate)
		sort.SliceStable(candidates, func(i, j int) bool {
			left := members[candidates[i]]
			right := members[candidates[j]]
			if left.Load != right.Load {
				return left.Load < right.Load
			}
			return left.Order < right.Order
		})

		for _, index := range candidates {
			if len(unit.Taken) >= perSlot {
				break
			}
			unit.Taken[index] = struct{}{}
			members[index].Load += 0.5
		}
	}

	// Merge parity coverage per (slot, member) into schedule labels.
	type parity struct {
		Name string
		Odd  bool
		Even bool
	}
	labelsBySlot := map[string][]parity{}
	for _, unit := range units {
		for index := range unit.Taken {
			member := members[index]
			existing := labelsBySlot[unit.ShiftCode]
			found := false
			for i := range existing {
				if existing[i].Name == member.Name {
					found = true
					if unit.OddWeek {
						existing[i].Odd = true
					} else {
						existing[i].Even = true
					}
					break
				}
			}
			if !found {
				entry := parity{Name: member.Name}
				if unit.OddWeek {
					entry.Odd = true
				} else {
					entry.Even = true
				}
				labelsBySlot[unit.ShiftCode] = append(existing, entry)
			}
		}
	}

	schedule := map[string][]string{}
	for code, entries := range labelsBySlot {
		labels := make([]string, 0, len(entries))
		for _, entry := range entries {
			switch {
			case entry.Odd && entry.Even:
				labels = append(labels, entry.Name+"(单双)")
			case entry.Odd:
				labels = append(labels, entry.Name+"(单)")
			default:
				labels = append(labels, entry.Name+"(双)")
			}
		}
		sort.Slice(labels, func(i, j int) bool { return config.LessRealName(baseName(labels[i]), baseName(labels[j])) })
		schedule[code] = labels
	}

	if len(schedule) == 0 {
		return types.AutoScheduleResponse{}, fmt.Errorf("空闲时间不足以生成任何排班，请先完善空闲登记")
	}

	return types.AutoScheduleResponse{
		Schedule:          schedule,
		ShiftDistribution: buildShiftDistribution(schedule),
		Warnings:          warnings,
	}, nil
}
