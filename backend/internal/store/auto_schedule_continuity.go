package store

import "personnel-management-go/internal/config"

type autoScheduleMovableAssignment struct {
	SlotIndex   int
	MemberIndex int
}

// optimizeAutoScheduleContinuity only exchanges generated assignments. Each
// exchange preserves coverage, per-member load, locked choices and pair count.
func optimizeAutoScheduleContinuity(slots []*autoScheduleSlot, members []autoScheduleMember) {
	optimizeAutoSchedulePairedContinuity(slots, members)
	optimizeAutoScheduleParityContinuity(slots, members, true)
	optimizeAutoScheduleParityContinuity(slots, members, false)
}

func optimizeAutoSchedulePairedContinuity(slots []*autoScheduleSlot, members []autoScheduleMember) {
	for {
		assignments := autoScheduleMovablePairs(slots, len(members))
		bestDelta := 0
		bestLeft := -1
		bestRight := -1

		for leftIndex := 0; leftIndex < len(assignments); leftIndex++ {
			left := assignments[leftIndex]
			for rightIndex := leftIndex + 1; rightIndex < len(assignments); rightIndex++ {
				right := assignments[rightIndex]
				if !autoScheduleCanSwapPair(slots, members, left, right) {
					continue
				}
				delta := autoScheduleContinuitySwapDelta(slots, left, right, true) +
					autoScheduleContinuitySwapDelta(slots, left, right, false)
				if delta > bestDelta {
					bestDelta = delta
					bestLeft = leftIndex
					bestRight = rightIndex
				}
			}
		}

		if bestDelta <= 0 {
			return
		}
		autoScheduleSwapPair(slots, assignments[bestLeft], assignments[bestRight])
	}
}

func optimizeAutoScheduleParityContinuity(slots []*autoScheduleSlot, members []autoScheduleMember, oddWeek bool) {
	for {
		assignments := autoScheduleMovableParityAssignments(slots, len(members), oddWeek)
		bestDelta := 0
		bestLeft := -1
		bestRight := -1

		for leftIndex := 0; leftIndex < len(assignments); leftIndex++ {
			left := assignments[leftIndex]
			for rightIndex := leftIndex + 1; rightIndex < len(assignments); rightIndex++ {
				right := assignments[rightIndex]
				if !autoScheduleCanSwapParity(slots, members, left, right, oddWeek) {
					continue
				}
				if autoSchedulePairDeltaForParitySwap(slots, left, right, oddWeek) != 0 {
					continue
				}
				delta := autoScheduleContinuitySwapDelta(slots, left, right, oddWeek)
				if delta > bestDelta {
					bestDelta = delta
					bestLeft = leftIndex
					bestRight = rightIndex
				}
			}
		}

		if bestDelta <= 0 {
			return
		}
		autoScheduleSwapParity(slots, assignments[bestLeft], assignments[bestRight], oddWeek)
	}
}

func autoScheduleMovablePairs(slots []*autoScheduleSlot, memberCount int) []autoScheduleMovableAssignment {
	assignments := make([]autoScheduleMovableAssignment, 0)
	for slotIndex, slot := range slots {
		for memberIndex := 0; memberIndex < memberCount; memberIndex++ {
			if !autoScheduleMemberTaken(slot, memberIndex, true) || !autoScheduleMemberTaken(slot, memberIndex, false) {
				continue
			}
			if autoScheduleMemberLocked(slot, memberIndex, true) || autoScheduleMemberLocked(slot, memberIndex, false) {
				continue
			}
			assignments = append(assignments, autoScheduleMovableAssignment{SlotIndex: slotIndex, MemberIndex: memberIndex})
		}
	}
	return assignments
}

func autoScheduleMovableParityAssignments(slots []*autoScheduleSlot, memberCount int, oddWeek bool) []autoScheduleMovableAssignment {
	assignments := make([]autoScheduleMovableAssignment, 0)
	for slotIndex, slot := range slots {
		for memberIndex := 0; memberIndex < memberCount; memberIndex++ {
			if !autoScheduleMemberTaken(slot, memberIndex, oddWeek) || autoScheduleMemberLocked(slot, memberIndex, oddWeek) {
				continue
			}
			assignments = append(assignments, autoScheduleMovableAssignment{SlotIndex: slotIndex, MemberIndex: memberIndex})
		}
	}
	return assignments
}

func autoScheduleCanSwapPair(slots []*autoScheduleSlot, members []autoScheduleMember, left, right autoScheduleMovableAssignment) bool {
	if left.SlotIndex == right.SlotIndex || left.MemberIndex == right.MemberIndex {
		return false
	}
	leftSlot := slots[left.SlotIndex]
	rightSlot := slots[right.SlotIndex]
	if autoScheduleMemberTaken(leftSlot, right.MemberIndex, true) || autoScheduleMemberTaken(leftSlot, right.MemberIndex, false) ||
		autoScheduleMemberTaken(rightSlot, left.MemberIndex, true) || autoScheduleMemberTaken(rightSlot, left.MemberIndex, false) {
		return false
	}
	return autoScheduleMemberAvailable(members[left.MemberIndex], rightSlot.Code, true) &&
		autoScheduleMemberAvailable(members[left.MemberIndex], rightSlot.Code, false) &&
		autoScheduleMemberAvailable(members[right.MemberIndex], leftSlot.Code, true) &&
		autoScheduleMemberAvailable(members[right.MemberIndex], leftSlot.Code, false)
}

func autoScheduleCanSwapParity(slots []*autoScheduleSlot, members []autoScheduleMember, left, right autoScheduleMovableAssignment, oddWeek bool) bool {
	if left.SlotIndex == right.SlotIndex || left.MemberIndex == right.MemberIndex {
		return false
	}
	leftSlot := slots[left.SlotIndex]
	rightSlot := slots[right.SlotIndex]
	if autoScheduleMemberTaken(leftSlot, right.MemberIndex, oddWeek) || autoScheduleMemberTaken(rightSlot, left.MemberIndex, oddWeek) {
		return false
	}
	return autoScheduleMemberAvailable(members[left.MemberIndex], rightSlot.Code, oddWeek) &&
		autoScheduleMemberAvailable(members[right.MemberIndex], leftSlot.Code, oddWeek)
}

func autoScheduleMemberAvailable(member autoScheduleMember, shiftCode string, oddWeek bool) bool {
	availability := member.Double
	if oddWeek {
		availability = member.Single
	}
	_, exists := availability[shiftCode]
	return exists
}

func autoSchedulePairDeltaForParitySwap(slots []*autoScheduleSlot, left, right autoScheduleMovableAssignment, oddWeek bool) int {
	leftOther := slots[left.SlotIndex].OddTaken
	rightOther := slots[right.SlotIndex].OddTaken
	if oddWeek {
		leftOther = slots[left.SlotIndex].EvenTaken
		rightOther = slots[right.SlotIndex].EvenTaken
	}
	return autoScheduleContainsDelta(leftOther, right.MemberIndex, left.MemberIndex) +
		autoScheduleContainsDelta(rightOther, left.MemberIndex, right.MemberIndex)
}

func autoScheduleContainsDelta(values map[int]struct{}, added, removed int) int {
	delta := 0
	if _, exists := values[added]; exists {
		delta++
	}
	if _, exists := values[removed]; exists {
		delta--
	}
	return delta
}

func autoScheduleContinuitySwapDelta(slots []*autoScheduleSlot, left, right autoScheduleMovableAssignment, oddWeek bool) int {
	return autoScheduleMemberContinuityDelta(slots, left.MemberIndex, oddWeek, left.SlotIndex, right.SlotIndex) +
		autoScheduleMemberContinuityDelta(slots, right.MemberIndex, oddWeek, right.SlotIndex, left.SlotIndex)
}

func autoScheduleMemberContinuityDelta(slots []*autoScheduleSlot, memberIndex int, oddWeek bool, removedSlot, addedSlot int) int {
	edgeStarts := [4]int{removedSlot - 1, removedSlot, addedSlot - 1, addedSlot}
	seen := map[int]struct{}{}
	delta := 0
	for _, edgeStart := range edgeStarts {
		if edgeStart < 0 || edgeStart+1 >= len(slots) || !autoScheduleSlotsShareDay(edgeStart, edgeStart+1) {
			continue
		}
		if _, exists := seen[edgeStart]; exists {
			continue
		}
		seen[edgeStart] = struct{}{}
		before := autoScheduleMemberTaken(slots[edgeStart], memberIndex, oddWeek) &&
			autoScheduleMemberTaken(slots[edgeStart+1], memberIndex, oddWeek)
		after := autoScheduleMemberTakenAfterMove(slots, memberIndex, oddWeek, edgeStart, removedSlot, addedSlot) &&
			autoScheduleMemberTakenAfterMove(slots, memberIndex, oddWeek, edgeStart+1, removedSlot, addedSlot)
		delta += boolToInt(after) - boolToInt(before)
	}
	return delta
}

func autoScheduleMemberTakenAfterMove(slots []*autoScheduleSlot, memberIndex int, oddWeek bool, slotIndex, removedSlot, addedSlot int) bool {
	if slotIndex == removedSlot {
		return false
	}
	if slotIndex == addedSlot {
		return true
	}
	return autoScheduleMemberTaken(slots[slotIndex], memberIndex, oddWeek)
}

func autoScheduleSlotsShareDay(leftSlot, rightSlot int) bool {
	shiftsPerDay := len(config.TimeSlots)
	return shiftsPerDay > 0 && leftSlot/shiftsPerDay == rightSlot/shiftsPerDay
}

func autoScheduleMemberTaken(slot *autoScheduleSlot, memberIndex int, oddWeek bool) bool {
	taken := slot.EvenTaken
	if oddWeek {
		taken = slot.OddTaken
	}
	_, exists := taken[memberIndex]
	return exists
}

func autoScheduleMemberLocked(slot *autoScheduleSlot, memberIndex int, oddWeek bool) bool {
	locked := slot.EvenLocked
	if oddWeek {
		locked = slot.OddLocked
	}
	_, exists := locked[memberIndex]
	return exists
}

func autoScheduleSwapPair(slots []*autoScheduleSlot, left, right autoScheduleMovableAssignment) {
	autoScheduleSwapParity(slots, left, right, true)
	autoScheduleSwapParity(slots, left, right, false)
}

func autoScheduleSwapParity(slots []*autoScheduleSlot, left, right autoScheduleMovableAssignment, oddWeek bool) {
	leftTaken := slots[left.SlotIndex].EvenTaken
	rightTaken := slots[right.SlotIndex].EvenTaken
	if oddWeek {
		leftTaken = slots[left.SlotIndex].OddTaken
		rightTaken = slots[right.SlotIndex].OddTaken
	}
	delete(leftTaken, left.MemberIndex)
	leftTaken[right.MemberIndex] = struct{}{}
	delete(rightTaken, right.MemberIndex)
	rightTaken[left.MemberIndex] = struct{}{}
}
