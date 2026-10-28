package http

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"personnel-management-go/internal/types"
)

func TestFinanceMemberPayIsOnlyReturnedForAuthorizedAggregateViews(t *testing.T) {
	s, cfg := newHTTPTestStore(t)
	defer s.Close()
	for _, role := range []string{"OWNER", "LEADER", "HR", "FINANCE", "USER", "VIEWER"} {
		if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "finance-" + role, RealName: "薪酬" + role, Role: role, InitialPassword: "initial-member-password"}); err != nil {
			t.Fatal(err)
		}
		user, err := s.GetUserByUsername("finance-" + role)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateOwnPassword(user.ID, "initial-member-password", "changed-member-password"); err != nil {
			t.Fatal(err)
		}
	}
	order, err := s.CreateWorkOrder(types.SaveWorkOrderRequest{Title: "薪酬权限工单", BelongingMonth: "2026-09", WorkSessions: []types.WorkSession{
		{Date: "2026-09-07", WorkerName: "薪酬OWNER", Duration: 2}, {Date: "2026-09-07", WorkerName: "薪酬USER", Duration: 1},
	}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(cfg, s)
	query := "/api/finance?startDate=2026-09-07&endDate=2026-09-08&includeManagement=true&managementMonths=1&workOrderIds=" + order.ID
	var ordinaryToken string
	for _, role := range []string{"ADMIN", "OWNER", "FINANCE", "LEADER", "HR", "USER", "VIEWER"} {
		t.Run(role, func(t *testing.T) {
			username, password := "finance-"+role, "changed-member-password"
			if role == "ADMIN" {
				username, password = "admin", "admin-password"
			}
			login := performJSONRequest(t, router, http.MethodPost, "/api/auth/login", "", types.LoginRequest{Username: username, Password: password})
			var auth types.LoginResponse
			if err := json.Unmarshal(login.Body.Bytes(), &auth); err != nil || login.Code != 200 {
				t.Fatalf("login failed: %s", login.Body.String())
			}
			if role == "USER" {
				ordinaryToken = auth.Token
			}
			for _, selected := range []string{"", "薪酬OWNER"} {
				response := performJSONRequest(t, router, http.MethodGet, query+"&realName="+url.QueryEscape(selected), auth.Token, nil)
				if role == "VIEWER" {
					if response.Code != http.StatusForbidden {
						t.Fatalf("viewer accessed finance: %d", response.Code)
					}
					continue
				}
				if response.Code != http.StatusOK {
					t.Fatalf("finance failed: %s", response.Body.String())
				}
				var summary types.FinanceSummaryResponse
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &summary); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
					t.Fatal(err)
				}
				authorized := role == "ADMIN" || role == "OWNER" || role == "FINANCE"
				if authorized && selected == "" {
					if len(summary.MemberSummaries) != 5 || summary.TotalAmount != 2950 {
						t.Fatalf("missing authorized aggregate details: %+v", summary)
					}
				} else if _, present := raw["memberSummaries"]; present {
					t.Fatal("member-pay rows leaked into a personal/restricted response")
				}
				if !authorized {
					want := float64(800)
					if role == "USER" {
						want = 50
					}
					if summary.TotalAmount != want {
						t.Fatalf("forged selection exposed another member's pay: got=%v want=%v", summary.TotalAmount, want)
					}
				} else if selected != "" && summary.TotalAmount != 1300 {
					t.Fatalf("selected-member pay differs: %+v", summary)
				}
			}
		})
	}
	// A missing own name must never turn a restricted request into an all-user query.
	db, err := sql.Open("sqlite", cfg.ControlDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE accounts SET real_name='' WHERE username='finance-USER'`); err != nil {
		t.Fatal(err)
	}
	response := performJSONRequest(t, router, http.MethodGet, query, ordinaryToken, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unnamed user accessed aggregate finance: %d %s", response.Code, response.Body.String())
	}
}
