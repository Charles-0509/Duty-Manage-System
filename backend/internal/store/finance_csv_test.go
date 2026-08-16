package store

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"testing"
	"time"

	"personnel-management-go/internal/types"
)

func TestFinanceCSVWriterKeepsSevenColumns(t *testing.T) {
	content, err := writeDutyCSVEntries([]dutyCSVEntry{
		{Name: "A", Date: mustCSVDate(t, "2026-06-08"), StartTime: "8:00", EndTime: "10:00", Hours: 2},
	})
	if err != nil {
		t.Fatalf("writeDutyCSVEntries returned error: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("generated CSV cannot be read: %v", err)
	}
	if got, want := len(rows[0]), 7; got != want {
		t.Fatalf("header columns = %d, want %d", got, want)
	}
	expected := []string{"姓名", "年", "月", "日", "起", "讫", "时数"}
	for index, want := range expected {
		if rows[0][index] != want {
			t.Fatalf("header[%d] = %q, want %q", index, rows[0][index], want)
		}
	}
}

func TestMapDateToOutputMonthKeepsDayAndClampsMonthEnd(t *testing.T) {
	outputMonth := mustCSVMonth(t, "2026-06")

	if got := mapDateToOutputMonth(mustCSVDate(t, "2026-05-27"), outputMonth); got.Format("2006-01-02") != "2026-06-27" {
		t.Fatalf("mapped 2026-05-27 = %s, want 2026-06-27", got.Format("2006-01-02"))
	}
	if got := mapDateToOutputMonth(mustCSVDate(t, "2026-05-31"), outputMonth); got.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("mapped 2026-05-31 = %s, want 2026-06-30", got.Format("2006-01-02"))
	}
}

func TestWorkOrderCSVHoursAreDoubledAndUseWorkdayBlocks(t *testing.T) {
	entries, err := buildFinanceCSVEntries(mustCSVMonth(t, "2026-06"), nil, []types.WorkOrder{
		{
			ID: "WO_1",
			WorkSessions: []types.WorkSession{
				{Date: "2026-06-08", WorkerName: "A", Duration: 4},
			},
		},
	}, nil, 0, DefaultRateConfig())
	if err != nil {
		t.Fatalf("buildFinanceCSVEntries returned error: %v", err)
	}

	assertCSVEntry(t, entries, "A", "2026-06-08", "8:00", "12:00", 4)
	assertCSVEntry(t, entries, "A", "2026-06-08", "14:00", "18:00", 4)
	if total := totalCSVHours(entries, "A"); total != 8 {
		t.Fatalf("total work order CSV hours = %.1f, want 8.0", total)
	}
}

func TestWorkOrderCSVAvoidsDutyConflictAndOverflowsToNextDay(t *testing.T) {
	entries, err := buildFinanceCSVEntries(mustCSVMonth(t, "2026-06"), []dutyCSVEntry{
		{Name: "A", Date: mustCSVDate(t, "2026-06-08"), StartTime: "8:00", EndTime: "12:00", Hours: 4},
	}, []types.WorkOrder{
		{
			ID: "WO_1",
			WorkSessions: []types.WorkSession{
				{Date: "2026-06-08", WorkerName: "A", Duration: 4},
			},
		},
	}, nil, 0, DefaultRateConfig())
	if err != nil {
		t.Fatalf("buildFinanceCSVEntries returned error: %v", err)
	}

	assertCSVEntry(t, entries, "A", "2026-06-08", "8:00", "12:00", 4)
	assertCSVEntry(t, entries, "A", "2026-06-08", "14:00", "18:00", 4)
	assertCSVEntry(t, entries, "A", "2026-06-09", "8:00", "12:00", 4)
	assertNoDuplicateCSVTime(t, entries)
}

func TestManagementCSVStartsAtFirstSaturdayAndUsesWeekends(t *testing.T) {
	entries, err := buildFinanceCSVEntries(mustCSVMonth(t, "2026-06"), nil, nil, []csvManagementPerson{
		{Name: "Leader", Role: "LEADER"},
	}, 1, DefaultRateConfig())
	if err != nil {
		t.Fatalf("buildFinanceCSVEntries returned error: %v", err)
	}

	expectedDays := []string{"2026-06-06", "2026-06-07", "2026-06-13", "2026-06-14"}
	for _, day := range expectedDays {
		assertCSVEntry(t, entries, "Leader", day, "8:00", "12:00", 4)
		assertCSVEntry(t, entries, "Leader", day, "14:00", "18:00", 4)
	}
	if total := totalCSVHours(entries, "Leader"); total != 32 {
		t.Fatalf("management CSV hours = %.1f, want 32.0", total)
	}
}

func TestCSVAllocatorAllowsExtendedHoursWithoutOverlap(t *testing.T) {
	allocator := newCSVScheduleAllocator()
	entries, err := allocator.allocate("A", mustCSVDate(t, "2026-06-08"), 10*60, []time.Time{mustCSVDate(t, "2026-06-08")}, csvNormalWorkBlocks(), csvExtendedWorkBlocks())
	if err != nil {
		t.Fatalf("allocate returned error: %v", err)
	}

	assertCSVEntry(t, entries, "A", "2026-06-08", "8:00", "12:00", 4)
	assertCSVEntry(t, entries, "A", "2026-06-08", "14:00", "18:00", 4)
	assertCSVEntry(t, entries, "A", "2026-06-08", "18:00", "20:00", 2)
	assertNoDuplicateCSVTime(t, entries)
}

func TestLaborAdjustedCSVUsesAdjustedAmountAsTwentyFiveYuanHours(t *testing.T) {
	content, err := createLaborAdjustedCSV(laborAdjustmentResult{
		People: []laborPerson{
			{Name: "A", Adjusted: 50000},
			{Name: "B", Adjusted: 25000},
		},
	}, "2026-06")
	if err != nil {
		t.Fatalf("createLaborAdjustedCSV returned error: %v", err)
	}

	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("generated CSV cannot be read: %v", err)
	}

	totals := map[string]float64{}
	entries := []dutyCSVEntry{}
	for _, row := range rows[1:] {
		if len(row) != 7 || row[1] == "合计" {
			continue
		}
		hours, err := strconv.ParseFloat(row[6], 64)
		if err != nil {
			t.Fatalf("parse hours %q: %v", row[6], err)
		}
		totals[row[0]] += hours
		entries = append(entries, dutyCSVEntry{
			Name:      row[0],
			Date:      mustCSVDate(t, row[1]+"-"+twoDigit(row[2])+"-"+twoDigit(row[3])),
			StartTime: row[4],
			EndTime:   row[5],
			Hours:     hours,
		})
	}

	if totals["A"] != 20 {
		t.Fatalf("A total CSV hours = %.1f, want 20.0", totals["A"])
	}
	if totals["B"] != 10 {
		t.Fatalf("B total CSV hours = %.1f, want 10.0", totals["B"])
	}
	assertNoDuplicateCSVTime(t, entries)
}

func TestLaborAdjustedCSVFromFinanceBatchKeepsRealDutyTimesFirst(t *testing.T) {
	entries, err := buildLaborAdjustedCSVEntriesWithPriority(mustCSVMonth(t, "2026-06"), laborAdjustmentResult{
		People: []laborPerson{
			{Name: "A", Adjusted: 25000},
		},
	}, []dutyCSVEntry{
		{Name: "A", Date: mustCSVDate(t, "2026-06-02"), StartTime: "13:30", EndTime: "15:30", Hours: 2},
		{Name: "A", Date: mustCSVDate(t, "2026-06-09"), StartTime: "13:30", EndTime: "15:30", Hours: 2},
		{Name: "A", Date: mustCSVDate(t, "2026-06-16"), StartTime: "13:30", EndTime: "15:30", Hours: 2},
		{Name: "A", Date: mustCSVDate(t, "2026-06-23"), StartTime: "13:30", EndTime: "15:30", Hours: 2},
		{Name: "A", Date: mustCSVDate(t, "2026-06-30"), StartTime: "13:30", EndTime: "15:30", Hours: 2},
	}, nil, nil, 0, DefaultRateConfig())
	if err != nil {
		t.Fatalf("buildLaborAdjustedCSVEntriesWithPriority returned error: %v", err)
	}

	expectedDays := []string{"2026-06-02", "2026-06-09", "2026-06-16", "2026-06-23", "2026-06-30"}
	for _, day := range expectedDays {
		assertCSVEntry(t, entries, "A", day, "13:30", "15:30", 2)
	}
	if total := totalCSVHours(entries, "A"); total != 10 {
		t.Fatalf("total adjusted CSV hours = %.1f, want 10.0", total)
	}
}

func assertCSVEntry(t *testing.T, entries []dutyCSVEntry, name string, date string, start string, end string, hours float64) {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == name && entry.Date.Format("2006-01-02") == date && entry.StartTime == start && entry.EndTime == end && entry.Hours == hours {
			return
		}
	}
	t.Fatalf("missing CSV entry %s %s %s-%s %.1f in %#v", name, date, start, end, hours, entries)
}

func assertNoDuplicateCSVTime(t *testing.T, entries []dutyCSVEntry) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, entry := range entries {
		key := entry.Name + "|" + entry.Date.Format("2006-01-02") + "|" + entry.StartTime + "|" + entry.EndTime
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate CSV time entry: %s", key)
		}
		seen[key] = struct{}{}
	}
}

func totalCSVHours(entries []dutyCSVEntry, name string) float64 {
	total := 0.0
	for _, entry := range entries {
		if entry.Name == name {
			total += entry.Hours
		}
	}
	return total
}

func mustCSVDate(t *testing.T, value string) time.Time {
	t.Helper()
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("parse date %s: %v", value, err)
	}
	return date
}

func mustCSVMonth(t *testing.T, value string) time.Time {
	t.Helper()
	month, err := time.Parse("2006-01", value)
	if err != nil {
		t.Fatalf("parse month %s: %v", value, err)
	}
	return month
}

func twoDigit(value string) string {
	if len(value) == 1 {
		return "0" + value
	}
	return value
}
