package store

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"
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
