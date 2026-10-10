package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/types"
)

func TestMonthlyReportHTTPIsAdminOnlyAndRespectsSemesterArchive(t *testing.T) {
	s, cfg := newHTTPTestStore(t)
	defer s.Close()
	router := NewRouter(cfg, s)
	login := performJSONRequest(t, router, http.MethodPost, "/api/auth/login", "", types.LoginRequest{Username: "admin", Password: "admin-password"})
	var session types.LoginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if login.Code != 200 {
		t.Fatalf("login: %s", login.Body.String())
	}
	body := types.MonthlyConditions{OutputMonth: "2026-09", StartDate: "2026-09-01", EndDate: "2026-09-30"}
	preview := performJSONRequest(t, router, http.MethodPost, "/api/monthly-reports/preview", session.Token, body)
	if preview.Code != 200 {
		t.Fatalf("preview %d: %s", preview.Code, preview.Body.String())
	}
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "monthly-owner", RealName: "月报负责人", Role: "OWNER", InitialPassword: "strong-member-password"}); err != nil {
		t.Fatal(err)
	}
	owner, err := s.GetUserByUsername("monthly-owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateOwnPassword(owner.ID, "strong-member-password", "another-member-password"); err != nil {
		t.Fatal(err)
	}
	owner, err = s.GetUserByUsername("monthly-owner")
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{UserID: owner.ID, SessionVersion: owner.SessionVersion, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	denied := performJSONRequest(t, router, http.MethodPost, "/api/monthly-reports/preview", token, body)
	if denied.Code != 403 {
		t.Fatalf("owner unexpectedly allowed: %d", denied.Code)
	}
	if err = s.SetSemesterArchived(s.ActiveSemester().ID, true); err != nil {
		t.Fatal(err)
	}
	locked := performJSONRequest(t, router, http.MethodPost, "/api/monthly-reports/preview", session.Token, body)
	if locked.Code != 423 {
		t.Fatalf("archive write=%d", locked.Code)
	}
	listed := performJSONRequest(t, router, http.MethodGet, "/api/monthly-reports", session.Token, nil)
	if listed.Code != 200 {
		t.Fatalf("archive read=%d", listed.Code)
	}
	for _, path := range []string{"/api/labor-convert/history", "/api/finance/export", "/api/finance/duty-csv"} {
		retired := performJSONRequest(t, router, http.MethodGet, path, session.Token, nil)
		if retired.Code != 404 {
			t.Fatalf("retired endpoint %s still exists (%d)", path, retired.Code)
		}
	}
}
