package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestWorkOrderAPIRejectsMissingDatesOnCreateAndUpdate(t *testing.T) {
	s, cfg := newHTTPTestStore(t)
	defer s.Close()
	if err := s.CreateSemesterMember(types.CreateMemberRequest{Username: "work-date", RealName: "日期测试成员", Role: "USER", InitialPassword: "strong-member-password"}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(cfg, s)
	login := performJSONRequest(t, router, http.MethodPost, "/api/auth/login", "", types.LoginRequest{Username: "admin", Password: "admin-password"})
	var session types.LoginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	valid := types.SaveWorkOrderRequest{Title: "日期校验", BelongingMonth: "2026-10", WorkSessions: []types.WorkSession{{Date: "2026-10-12", WorkerName: "日期测试成员", Duration: 2}}}
	created := performJSONRequest(t, router, http.MethodPost, "/api/work-orders", session.Token, valid)
	if created.Code != http.StatusCreated {
		t.Fatalf("valid order rejected: %s", created.Body.String())
	}
	var order types.WorkOrder
	if err := json.Unmarshal(created.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.WorkSessions = append(append([]types.WorkSession{}, valid.WorkSessions...), types.WorkSession{WorkerName: "日期测试成员", Duration: 1})
	for _, route := range []struct{ method, path string }{{http.MethodPost, "/api/work-orders"}, {http.MethodPut, "/api/work-orders/" + order.ID}} {
		response := performJSONRequest(t, router, route.method, route.path, session.Token, invalid)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "第2条") || !strings.Contains(response.Body.String(), "日期不能为空") {
			t.Fatalf("invalid date was not explained: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	dashboard, err := s.GetDashboard(true)
	if err != nil || dashboard.WorkOrderCount != 1 {
		t.Fatalf("invalid create wrote data: %+v, %v", dashboard, err)
	}
}
