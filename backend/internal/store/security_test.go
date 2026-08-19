package store

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"personnel-management-go/internal/types"
)

func TestParseAllowedDateRangeRejectsAbsurdSpan(t *testing.T) {
	_, _, err := parseAllowedDateRange("2026-04-01", "9999-12-31")
	if err == nil {
		t.Fatal("multi-millennium range was accepted")
	}
	if !errors.Is(err, ErrMonthOutOfRange) {
		t.Fatalf("expected ErrMonthOutOfRange for out-of-window dates, got %v", err)
	}

	_, _, err = parseAllowedDateRange("2026-04-01", "2030-04-02")
	if !errors.Is(err, ErrDateRangeTooWide) {
		t.Fatalf("expected ErrDateRangeTooWide, got %v", err)
	}

	start, end, err := parseAllowedDateRange("2026-04-01", "2027-03-01")
	if err != nil {
		t.Fatalf("valid 1-year range rejected: %v", err)
	}
	if !start.Before(end) {
		t.Fatalf("unexpected range %v - %v", start, end)
	}
}

func TestParseAllowedDateRangeBoundsToAllowedMonths(t *testing.T) {
	if _, _, err := parseAllowedDateRange("2026-03-01", "2026-04-01"); !errors.Is(err, ErrMonthOutOfRange) {
		t.Fatalf("start before allowed month window accepted: %v", err)
	}
	if _, _, err := parseAllowedDateRange("2050-04-01", "2051-01-01"); !errors.Is(err, ErrMonthOutOfRange) {
		t.Fatalf("end after allowed month window accepted: %v", err)
	}
}

func TestSanitizeCSVCellNeutralizesFormulaPrefixes(t *testing.T) {
	cases := map[string]string{
		"=1+1":        "'=1+1",
		"+SUM(A1)":    "'+SUM(A1)",
		"-2+CMD":      "'-2+CMD",
		"@cmd":        "'@cmd",
		"\ttab":       "'\ttab",
		"张三":          "张三",
		"":            "",
		"普通 normal 1": "普通 normal 1",
	}
	for input, want := range cases {
		if got := sanitizeCSVCell(input); got != want {
			t.Fatalf("sanitizeCSVCell(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWriteDutyCSVEntriesSanitizesNames(t *testing.T) {
	content, err := writeDutyCSVEntries([]dutyCSVEntry{
		{Name: "=HYPERLINK(\"http://evil\")", Date: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC), StartTime: "8:00", EndTime: "10:00", Hours: 2},
	})
	if err != nil {
		t.Fatalf("writeDutyCSVEntries: %v", err)
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][0] != "'=HYPERLINK(\"http://evil\")" {
		t.Fatalf("formula name was not neutralized: %q", rows[1][0])
	}
}

func TestValidRealName(t *testing.T) {
	valid := []string{"张三", "O'Brien", "让-保罗", "A.B"}
	for _, name := range valid {
		if !validRealName(name) {
			t.Fatalf("validRealName(%q) = false, want true", name)
		}
	}
	invalid := []string{
		"", " 张三", "张三 ", "a/b", "a\\b", "a:b", "../..", "a..b",
		"名字名字名字名字名字名字名字名字名字名字名字名字名字名字名字名字名字", // 34 runes
		"换\n行",
	}
	for _, name := range invalid {
		if validRealName(name) {
			t.Fatalf("validRealName(%q) = true, want false", name)
		}
	}
}

func TestRateConfigValidation(t *testing.T) {
	if err := (RateConfig{DutyCents: 2500, WorkOrderCents: 5000, MgmtLeaderCents: 80000, MgmtOwnerCents: 120000}).validate(); err != nil {
		t.Fatalf("default rates rejected: %v", err)
	}
	if err := (RateConfig{DutyCents: 0, WorkOrderCents: 5000}).validate(); err == nil {
		t.Fatal("zero duty rate accepted")
	}
	if err := (RateConfig{DutyCents: 2500, WorkOrderCents: 5000, MgmtLeaderCents: rateMaxCents + 1, MgmtOwnerCents: 0}).validate(); err == nil {
		t.Fatal("oversized management rate accepted")
	}
}

func TestRefreshTokenRotationAndRevocation(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	users, err := appStore.ListUsers()
	if err != nil || len(users) == 0 {
		t.Fatalf("ListUsers: %v (items=%d)", err, len(users))
	}
	var accountID int64
	for _, user := range users {
		if user.Username == "admin" {
			accountID = user.ID
		}
	}
	if accountID == 0 {
		t.Fatal("admin account not found")
	}

	first, err := appStore.IssueRefreshToken(accountID)
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}
	returnedID, second, err := appStore.RotateRefreshToken(first)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if returnedID != accountID {
		t.Fatalf("rotated token belongs to account %d, want %d", returnedID, accountID)
	}

	// Reuse of the rotated token must fail.
	if _, _, err := appStore.RotateRefreshToken(first); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("reused refresh token accepted: %v", err)
	}

	// Bumping the session version invalidates outstanding access tokens.
	version, err := appStore.sessionVersionForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if version < 1 {
		t.Fatalf("session version starts at %d, want >= 1", version)
	}
	if err := appStore.BumpSessionVersion(accountID); err != nil {
		t.Fatal(err)
	}
	next, err := appStore.sessionVersionForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if next != version+1 {
		t.Fatalf("session version after bump = %d, want %d", next, version+1)
	}

	appStore.RevokeAccountRefreshTokens(accountID)
	if _, _, err := appStore.RotateRefreshToken(second); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Fatalf("revoked refresh token accepted: %v", err)
	}
}

func TestInsertAuditLogBoundsTextFields(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()

	longValue := strings.Repeat("界", 300)
	if err := appStore.InsertAuditLog(types.AuditLogEntry{
		Username: longValue, RealName: longValue, Action: longValue,
		Status: 200, SemesterID: longValue, IP: longValue,
	}); err != nil {
		t.Fatal(err)
	}
	logs, err := appStore.ListAuditLogs(1, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs.Items) != 1 {
		t.Fatalf("audit rows=%d, want 1", len(logs.Items))
	}
	item := logs.Items[0]
	for field, value := range map[string]string{
		"username": item.Username, "real name": item.RealName, "action": item.Action,
		"semester ID": item.SemesterID, "IP": item.IP,
	} {
		if !utf8.ValidString(value) {
			t.Fatalf("%s was truncated to invalid UTF-8", field)
		}
	}
	if len(item.Username) > auditUsernameMaxBytes || len(item.RealName) > auditRealNameMaxBytes ||
		len(item.Action) > auditActionMaxBytes || len(item.SemesterID) > auditSemesterIDMaxBytes || len(item.IP) > auditIPMaxBytes {
		t.Fatalf("audit fields exceeded limits: %+v", item)
	}
}
