package store

import (
	"fmt"
	"sort"
	"strings"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"
)

const (
	autoScheduleMinPerSlot = 1
	autoScheduleMaxPerSlot = 3
	autoScheduleSoftLoad   = 4.0
	autoScheduleHardLoad   = 5.0

	autoScheduleSoftUnits = int(autoScheduleSoftLoad * 2)
	autoScheduleHardUnits = int(autoScheduleHardLoad * 2)
)

type autoScheduleMember struct {
	Name              string
	Single            map[string]struct{}
	Double            map[string]struct{}
	Order             int
	AvailabilityUnits int
}

type autoScheduleSlot struct {
	Code           string
	Order          int
	OddCandidates  []int
	EvenCandidates []int
	PairCandidates []int
	OddTaken       map[int]struct{}
	EvenTaken      map[int]struct{}
	OddLocked      map[int]struct{}
	EvenLocked     map[int]struct{}
}

type autoScheduleEdgeRef struct {
	From        int
	EdgeIndex   int
	SlotIndex   int
	MemberIndex int
	OddWeek     bool
}

// GenerateAutoSchedule builds a proposal from the current semester's
// availability. Saving remains a separate action so an administrator can
// review and adjust the result first.
func (s *Store) GenerateAutoSchedule(perSlot int, lockedSchedule map[string][]string) (types.AutoScheduleResponse, error) {
	overview, err := s.GetAvailabilityOverview()
	if err != nil {
		return types.AutoScheduleResponse{}, err
	}
	return generateAutoScheduleFromOverviewWithLocked(overview, perSlot, lockedSchedule)
}

func generateAutoScheduleFromOverview(overview []types.AvailabilityOverviewItem, perSlot int) (types.AutoScheduleResponse, error) {
	return generateAutoScheduleFromOverviewWithLocked(overview, perSlot, nil)
}

func generateAutoScheduleFromOverviewWithLocked(overview []types.AvailabilityOverviewItem, perSlot int, lockedSchedule map[string][]string) (types.AutoScheduleResponse, error) {
	if perSlot < autoScheduleMinPerSlot || perSlot > autoScheduleMaxPerSlot {
		return types.AutoScheduleResponse{}, fmt.Errorf("每班人数必须是 %d 到 %d 之间的整数", autoScheduleMinPerSlot, autoScheduleMaxPerSlot)
	}

	members := buildAutoScheduleMembers(overview)
	if len(members) == 0 {
		return types.AutoScheduleResponse{}, fmt.Errorf("还没有成员登记空闲时间，无法自动排班")
	}
	slots := buildAutoScheduleSlots(members)
	loadUnits, err := applyLockedAutoSchedule(slots, members, lockedSchedule)
	if err != nil {
		return types.AutoScheduleResponse{}, err
	}

	unlimitedUnits := len(slots) * 2
	targetCoverage := autoScheduleRemainingMaxFlow(slots, len(members), perSlot, unlimitedUnits, loadUnits)
	if targetCoverage == 0 && autoScheduleTakenCount(slots) == 0 {
		return types.AutoScheduleResponse{}, fmt.Errorf("空闲时间不足以生成任何排班，请先完善空闲登记")
	}

	capacityUnits := unlimitedUnits
	if autoScheduleRemainingMaxFlow(slots, len(members), perSlot, autoScheduleSoftUnits, loadUnits) == targetCoverage {
		capacityUnits = autoScheduleSoftUnits
	} else if autoScheduleRemainingMaxFlow(slots, len(members), perSlot, autoScheduleHardUnits, loadUnits) == targetCoverage {
		capacityUnits = autoScheduleHardUnits
	}

	pairedAssignments := forcePreferredPairs(slots, members, perSlot, capacityUnits, targetCoverage, loadUnits)
	remainingTarget := targetCoverage - pairedAssignments
	if remainingTarget > 0 {
		flow, refs := buildAutoScheduleCostFlow(slots, members, perSlot, capacityUnits, loadUnits)
		assigned, _ := flow.minCostMaxFlow(remainingTarget)
		if assigned != remainingTarget {
			return types.AutoScheduleResponse{}, fmt.Errorf("自动排班内部校验失败：无法保持最大覆盖")
		}
		applyAutoScheduleFlow(slots, flow, refs)
	}
	optimizeAutoScheduleContinuity(slots, members)

	schedule := autoScheduleLabels(slots, members)
	warnings := autoScheduleCoverageWarnings(slots, perSlot)
	distribution := buildShiftDistribution(schedule)
	if warning := autoScheduleHardCapWarning(distribution); warning != "" {
		warnings = append(warnings, warning)
	}

	return types.AutoScheduleResponse{
		Schedule:          schedule,
		ShiftDistribution: distribution,
		Warnings:          warnings,
	}, nil
}

func buildAutoScheduleMembers(overview []types.AvailabilityOverviewItem) []autoScheduleMember {
	members := make([]autoScheduleMember, 0, len(overview))
	for index, item := range overview {
		member := autoScheduleMember{
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
		member.AvailabilityUnits = len(member.Single) + len(member.Double)
		members = append(members, member)
	}
	return members
}

func buildAutoScheduleSlots(members []autoScheduleMember) []*autoScheduleSlot {
	slots := make([]*autoScheduleSlot, 0, len(config.WeekdaysCode)*len(config.TimeSlots))
	for _, dayCode := range config.WeekdaysCode {
		for shiftIndex := range config.TimeSlots {
			code := fmt.Sprintf("%s-%d", dayCode, shiftIndex+1)
			slot := &autoScheduleSlot{
				Code:       code,
				Order:      len(slots),
				OddTaken:   map[int]struct{}{},
				EvenTaken:  map[int]struct{}{},
				OddLocked:  map[int]struct{}{},
				EvenLocked: map[int]struct{}{},
			}
			for memberIndex, member := range members {
				_, odd := member.Single[code]
				_, even := member.Double[code]
				if odd {
					slot.OddCandidates = append(slot.OddCandidates, memberIndex)
				}
				if even {
					slot.EvenCandidates = append(slot.EvenCandidates, memberIndex)
				}
				if odd && even {
					slot.PairCandidates = append(slot.PairCandidates, memberIndex)
				}
			}
			slots = append(slots, slot)
		}
	}
	return slots
}

func applyLockedAutoSchedule(slots []*autoScheduleSlot, members []autoScheduleMember, lockedSchedule map[string][]string) ([]int, error) {
	loadUnits := make([]int, len(members))
	if len(lockedSchedule) == 0 {
		return loadUnits, nil
	}

	memberIndexByName := make(map[string]int, len(members))
	for index, member := range members {
		memberIndexByName[member.Name] = index
	}
	slotByCode := make(map[string]*autoScheduleSlot, len(slots))
	for _, slot := range slots {
		slotByCode[slot.Code] = slot
	}

	for shiftCode, labels := range lockedSchedule {
		if len(labels) == 0 {
			continue
		}
		slot, exists := slotByCode[shiftCode]
		if !exists {
			return nil, fmt.Errorf("手动排班包含无效班次 %s", shiftCode)
		}
		for _, label := range uniqueStrings(labels) {
			realName, weekType := parseScheduleLabel(label)
			memberIndex, exists := memberIndexByName[realName]
			if !exists {
				return nil, fmt.Errorf("手动排班成员 %s 已不属于当前学期", realName)
			}
			if weekType == "single" || weekType == "both" {
				if _, taken := slot.OddTaken[memberIndex]; !taken {
					slot.OddTaken[memberIndex] = struct{}{}
					loadUnits[memberIndex]++
				}
				slot.OddLocked[memberIndex] = struct{}{}
			}
			if weekType == "double" || weekType == "both" {
				if _, taken := slot.EvenTaken[memberIndex]; !taken {
					slot.EvenTaken[memberIndex] = struct{}{}
					loadUnits[memberIndex]++
				}
				slot.EvenLocked[memberIndex] = struct{}{}
			}
		}
	}
	return loadUnits, nil
}

func autoScheduleTakenCount(slots []*autoScheduleSlot) int {
	total := 0
	for _, slot := range slots {
		total += len(slot.OddTaken) + len(slot.EvenTaken)
	}
	return total
}

func forcePreferredPairs(slots []*autoScheduleSlot, members []autoScheduleMember, perSlot, capacityUnits, targetCoverage int, loadUnits []int) int {
	orderedSlots := append([]*autoScheduleSlot(nil), slots...)
	sort.SliceStable(orderedSlots, func(i, j int) bool {
		left := orderedSlots[i]
		right := orderedSlots[j]
		leftMin := min(len(left.OddCandidates), len(left.EvenCandidates))
		rightMin := min(len(right.OddCandidates), len(right.EvenCandidates))
		if leftMin != rightMin {
			return leftMin < rightMin
		}
		leftMax := max(len(left.OddCandidates), len(left.EvenCandidates))
		rightMax := max(len(right.OddCandidates), len(right.EvenCandidates))
		if leftMax != rightMax {
			return leftMax < rightMax
		}
		if len(left.PairCandidates) != len(right.PairCandidates) {
			return len(left.PairCandidates) < len(right.PairCandidates)
		}
		return left.Order < right.Order
	})

	pairedAssignments := 0
	for _, slot := range orderedSlots {
		for pairedAssignments+2 <= targetCoverage && autoScheduleDeficit(perSlot, len(slot.OddTaken)) > 0 && autoScheduleDeficit(perSlot, len(slot.EvenTaken)) > 0 {
			candidates := append([]int(nil), slot.PairCandidates...)
			sort.SliceStable(candidates, func(i, j int) bool {
				return lessAutoScheduleMember(candidates[i], candidates[j], members, loadUnits)
			})

			accepted := false
			for _, memberIndex := range candidates {
				_, oddTaken := slot.OddTaken[memberIndex]
				_, evenTaken := slot.EvenTaken[memberIndex]
				if oddTaken || evenTaken || loadUnits[memberIndex]+2 > capacityUnits {
					continue
				}
				slot.OddTaken[memberIndex] = struct{}{}
				slot.EvenTaken[memberIndex] = struct{}{}
				loadUnits[memberIndex] += 2
				remainingTarget := targetCoverage - pairedAssignments - 2
				if autoScheduleRemainingMaxFlow(slots, len(members), perSlot, capacityUnits, loadUnits) >= remainingTarget {
					pairedAssignments += 2
					accepted = true
					break
				}
				delete(slot.OddTaken, memberIndex)
				delete(slot.EvenTaken, memberIndex)
				loadUnits[memberIndex] -= 2
			}
			if !accepted {
				break
			}
		}
	}
	return pairedAssignments
}

func autoScheduleDeficit(perSlot, assigned int) int {
	return max(0, perSlot-assigned)
}

func lessAutoScheduleMember(leftIndex, rightIndex int, members []autoScheduleMember, loadUnits []int) bool {
	left := members[leftIndex]
	right := members[rightIndex]
	if loadUnits[leftIndex] != loadUnits[rightIndex] {
		return loadUnits[leftIndex] < loadUnits[rightIndex]
	}
	leftScarcity := autoScheduleScarcityRank(left.AvailabilityUnits)
	rightScarcity := autoScheduleScarcityRank(right.AvailabilityUnits)
	if leftScarcity != rightScarcity {
		return leftScarcity < rightScarcity
	}
	if left.AvailabilityUnits != right.AvailabilityUnits {
		return left.AvailabilityUnits < right.AvailabilityUnits
	}
	return left.Order < right.Order
}

func autoScheduleScarcityRank(availabilityUnits int) int {
	switch {
	case availabilityUnits <= 4:
		return 0
	case availabilityUnits <= 6:
		return 1
	default:
		return 2
	}
}

func buildAutoScheduleCostFlow(slots []*autoScheduleSlot, members []autoScheduleMember, perSlot, capacityUnits int, forcedLoad []int) (*autoScheduleCostFlow, []autoScheduleEdgeRef) {
	source := 0
	slotNodeStart := 1
	memberNodeStart := slotNodeStart + len(slots)*2
	sink := memberNodeStart + len(members)
	flow := newAutoScheduleCostFlow(sink+1, source, sink)
	refs := make([]autoScheduleEdgeRef, 0)

	for slotIndex, slot := range slots {
		for parity := 0; parity < 2; parity++ {
			oddWeek := parity == 0
			node := slotNodeStart + slotIndex*2 + parity
			remainingDemand := autoScheduleDeficit(perSlot, len(slot.EvenTaken))
			if oddWeek {
				remainingDemand = autoScheduleDeficit(perSlot, len(slot.OddTaken))
			}
			if remainingDemand <= 0 {
				continue
			}
			flow.addEdge(source, node, remainingDemand, 0)
			candidates := slot.EvenCandidates
			if oddWeek {
				candidates = slot.OddCandidates
			}
			for _, memberIndex := range candidates {
				taken := slot.EvenTaken
				if oddWeek {
					taken = slot.OddTaken
				}
				if _, exists := taken[memberIndex]; exists {
					continue
				}
				cost := autoScheduleMemberPreferenceCost(members[memberIndex])
				edgeIndex := flow.addEdge(node, memberNodeStart+memberIndex, 1, cost)
				refs = append(refs, autoScheduleEdgeRef{
					From:        node,
					EdgeIndex:   edgeIndex,
					SlotIndex:   slotIndex,
					MemberIndex: memberIndex,
					OddWeek:     oddWeek,
				})
			}
		}
	}

	for memberIndex := range members {
		remainingCapacity := capacityUnits - forcedLoad[memberIndex]
		for unit := 1; unit <= remainingCapacity; unit++ {
			resultingUnit := forcedLoad[memberIndex] + unit
			flow.addEdge(memberNodeStart+memberIndex, sink, 1, autoScheduleLoadCost(resultingUnit))
		}
	}
	return flow, refs
}

func autoScheduleMemberPreferenceCost(member autoScheduleMember) int {
	return autoScheduleScarcityRank(member.AvailabilityUnits)*100 + min(member.AvailabilityUnits, 99)*2 + member.Order
}

func autoScheduleLoadCost(resultingUnit int) int {
	cost := resultingUnit * 10_000
	if resultingUnit > autoScheduleSoftUnits {
		cost += 1_000_000
	}
	if resultingUnit > autoScheduleHardUnits {
		cost += 10_000_000
	}
	return cost
}

func applyAutoScheduleFlow(slots []*autoScheduleSlot, flow *autoScheduleCostFlow, refs []autoScheduleEdgeRef) {
	for _, ref := range refs {
		if flow.graph[ref.From][ref.EdgeIndex].capacity != 0 {
			continue
		}
		if ref.OddWeek {
			slots[ref.SlotIndex].OddTaken[ref.MemberIndex] = struct{}{}
		} else {
			slots[ref.SlotIndex].EvenTaken[ref.MemberIndex] = struct{}{}
		}
	}
}

func autoScheduleLabels(slots []*autoScheduleSlot, members []autoScheduleMember) map[string][]string {
	schedule := make(map[string][]string, len(slots))
	for _, slot := range slots {
		labels := make([]string, 0, len(slot.OddTaken)+len(slot.EvenTaken))
		for memberIndex, member := range members {
			_, odd := slot.OddTaken[memberIndex]
			_, even := slot.EvenTaken[memberIndex]
			switch {
			case odd && even:
				labels = append(labels, member.Name+"(单双)")
			case odd:
				labels = append(labels, member.Name+"(单)")
			case even:
				labels = append(labels, member.Name+"(双)")
			}
		}
		if len(labels) == 0 {
			continue
		}
		sort.Slice(labels, func(i, j int) bool {
			return config.LessRealName(baseName(labels[i]), baseName(labels[j]))
		})
		schedule[slot.Code] = labels
	}
	return schedule
}

func autoScheduleCoverageWarnings(slots []*autoScheduleSlot, perSlot int) []string {
	warnings := make([]string, 0)
	for _, slot := range slots {
		for _, week := range []struct {
			label    string
			assigned int
		}{
			{label: "单周", assigned: len(slot.OddTaken)},
			{label: "双周", assigned: len(slot.EvenTaken)},
		} {
			switch {
			case week.assigned == 0:
				warnings = append(warnings, fmt.Sprintf("%s %s未排到人员", slot.Code, week.label))
			case week.assigned < perSlot:
				warnings = append(warnings, fmt.Sprintf("%s %s仅排 %d 人，少于每班 %d 人", slot.Code, week.label, week.assigned, perSlot))
			case week.assigned > perSlot:
				warnings = append(warnings, fmt.Sprintf("%s %s已有 %d 人，超过每班 %d 人；已保留手动排班", slot.Code, week.label, week.assigned, perSlot))
			}
		}
	}
	return warnings
}

func autoScheduleHardCapWarning(distribution []types.ChartItem) string {
	overloaded := make([]string, 0)
	for _, item := range distribution {
		if item.Value > autoScheduleHardLoad {
			overloaded = append(overloaded, fmt.Sprintf("%s %.1f班", item.Name, item.Value))
		}
	}
	if len(overloaded) == 0 {
		return ""
	}
	return "为保留手动排班或保证班次覆盖，以下成员超过 5 班：" + strings.Join(overloaded, "、")
}
