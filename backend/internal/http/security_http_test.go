package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/types"
)

func TestTunnelClientIPsHaveIndependentLoginBudgets(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	for attempt := 0; attempt < 5; attempt++ {
		response := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:41000", "198.51.100.10", map[string]any{
			"username": "missing-user",
			"password": "wrong-password",
		})
		wantStatus := stdhttp.StatusUnauthorized
		if attempt == 4 {
			wantStatus = stdhttp.StatusTooManyRequests
		}
		if response.Code != wantStatus {
			t.Fatalf("failed login %d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
		if attempt < 4 && !strings.Contains(response.Body.String(), fmt.Sprintf("还剩%d次机会", 4-attempt)) {
			t.Fatalf("failed login %d missing remaining-attempt message: %s", attempt+1, response.Body.String())
		}
	}

	otherClient := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:41001", "198.51.100.11", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	if otherClient.Code != stdhttp.StatusOK {
		t.Fatalf("independent tunnel client status=%d body=%s", otherClient.Code, otherClient.Body.String())
	}

	blockedClient := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:41002", "198.51.100.10", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	if blockedClient.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("blocked client status=%d body=%s", blockedClient.Code, blockedClient.Body.String())
	}

	directLAN := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "192.168.1.25:42000", "203.0.113.99", map[string]any{
		"username": "another-missing-user",
		"password": "wrong-password",
	})
	if directLAN.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("direct LAN login status=%d body=%s", directLAN.Code, directLAN.Body.String())
	}

	logs, err := appStore.ListAuditLogs(1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	foundTunnelIP := false
	foundLANIP := false
	for _, item := range logs.Items {
		foundTunnelIP = foundTunnelIP || item.IP == "198.51.100.11"
		foundLANIP = foundLANIP || item.IP == "192.168.1.25"
		if item.IP == "203.0.113.99" {
			t.Fatal("direct LAN request was allowed to spoof CF-Connecting-IP")
		}
	}
	if !foundTunnelIP || !foundLANIP {
		t.Fatalf("audit IPs missing: tunnel=%v LAN=%v items=%+v", foundTunnelIP, foundLANIP, logs.Items)
	}
}

func TestAdminPasswordResetImmediatelyClearsLoginRestriction(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "locked-member", RealName: "锁定成员", Role: "USER", InitialPassword: "initial-member-password",
	}); err != nil {
		t.Fatal(err)
	}
	member, err := appStore.GetUserByUsername("locked-member")
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 5; attempt++ {
		response := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:43000", "198.51.100.50", map[string]any{
			"username": "locked-member",
			"password": "wrong-password",
		})
		if attempt == 4 && response.Code != stdhttp.StatusTooManyRequests {
			t.Fatalf("lock status=%d body=%s", response.Code, response.Body.String())
		}
	}

	adminResponse := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:43001", "198.51.100.51", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	var admin types.LoginResponse
	if err := json.Unmarshal(adminResponse.Body.Bytes(), &admin); err != nil {
		t.Fatal(err)
	}
	reset := performJSONRequest(t, router, stdhttp.MethodPatch, "/api/users/"+strconv.FormatInt(member.ID, 10)+"/password", admin.Token, map[string]any{
		"newPassword": "reset-member-password",
	})
	if reset.Code != stdhttp.StatusOK || !strings.Contains(reset.Body.String(), "登录限制") {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}

	unlocked := performJSONRequestFrom(t, router, stdhttp.MethodPost, "/api/auth/login", "127.0.0.1:43002", "198.51.100.50", map[string]any{
		"username": "locked-member",
		"password": "reset-member-password",
	})
	if unlocked.Code != stdhttp.StatusOK {
		t.Fatalf("unlocked login status=%d body=%s", unlocked.Code, unlocked.Body.String())
	}
}

func TestAuthRequestBodyLimit(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	body := `{"username":"` + strings.Repeat("a", int(authRequestMaxBytes)) + `","password":"x"}`
	request := httptest.NewRequest(stdhttp.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != stdhttp.StatusRequestEntityTooLarge {
		t.Fatalf("oversized auth body status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestForcedPasswordChangeAndDashboardAuthorization(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "member-one", RealName: "成员甲", Role: "USER", InitialPassword: "initial-member-password",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
		Title:          "安全测试工单",
		BelongingMonth: time.Now().Format("2006-01"),
		WorkSessions: []types.WorkSession{{
			Date: time.Now().Format("2006-01-02"), WorkerName: "成员甲", Duration: 1,
		}},
	}, "系统管理员"); err != nil {
		t.Fatal(err)
	}

	loginResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "member-one",
		"password": "initial-member-password",
	})
	if loginResponse.Code != stdhttp.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var login types.LoginResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}

	blocked := performJSONRequest(t, router, stdhttp.MethodGet, "/api/dashboard", login.Token, nil)
	if blocked.Code != stdhttp.StatusForbidden {
		t.Fatalf("forced-change dashboard status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	weakChange := performJSONRequest(t, router, stdhttp.MethodPut, "/api/auth/password", login.Token, map[string]any{
		"currentPassword": "initial-member-password",
		"newPassword":     "short",
	})
	if weakChange.Code != stdhttp.StatusBadRequest {
		t.Fatalf("weak password status=%d body=%s", weakChange.Code, weakChange.Body.String())
	}
	strongChange := performJSONRequest(t, router, stdhttp.MethodPut, "/api/auth/password", login.Token, map[string]any{
		"currentPassword": "initial-member-password",
		"newPassword":     "changed-member-password",
	})
	if strongChange.Code != stdhttp.StatusOK {
		t.Fatalf("strong password status=%d body=%s", strongChange.Code, strongChange.Body.String())
	}
	var changed types.ChangePasswordResponse
	if err := json.Unmarshal(strongChange.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}

	memberDashboard := performJSONRequest(t, router, stdhttp.MethodGet, "/api/dashboard", changed.Token, nil)
	if memberDashboard.Code != stdhttp.StatusOK {
		t.Fatalf("member dashboard status=%d body=%s", memberDashboard.Code, memberDashboard.Body.String())
	}
	var memberData types.DashboardResponse
	if err := json.Unmarshal(memberDashboard.Body.Bytes(), &memberData); err != nil {
		t.Fatal(err)
	}
	if memberData.WorkOrderCount != 0 || len(memberData.WorkDurationShare) != 0 {
		t.Fatalf("member received work-order metrics: %+v", memberData)
	}

	adminLogin := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	if adminLogin.Code != stdhttp.StatusOK {
		t.Fatalf("admin login status=%d body=%s", adminLogin.Code, adminLogin.Body.String())
	}
	var admin types.LoginResponse
	if err := json.Unmarshal(adminLogin.Body.Bytes(), &admin); err != nil {
		t.Fatal(err)
	}
	adminDashboard := performJSONRequest(t, router, stdhttp.MethodGet, "/api/dashboard", admin.Token, nil)
	var adminData types.DashboardResponse
	if err := json.Unmarshal(adminDashboard.Body.Bytes(), &adminData); err != nil {
		t.Fatal(err)
	}
	if adminData.WorkOrderCount != 1 || len(adminData.WorkDurationShare) != 1 {
		t.Fatalf("admin work-order metrics changed: %+v", adminData)
	}

	weakReset := performJSONRequest(t, router, stdhttp.MethodPatch, "/api/users/"+strconv.FormatInt(admin.User.ID, 10)+"/password", admin.Token, map[string]any{
		"newPassword": "short",
	})
	if weakReset.Code != stdhttp.StatusBadRequest {
		t.Fatalf("weak reset status=%d body=%s", weakReset.Code, weakReset.Body.String())
	}
	if _, err := appStore.ResetPassword(admin.User.ID, "reset-admin-password"); err != nil {
		t.Fatal(err)
	}
	resetLogin := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "reset-admin-password",
	})
	var resetAdmin types.LoginResponse
	if err := json.Unmarshal(resetLogin.Body.Bytes(), &resetAdmin); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/semesters", "/api/audit-logs"} {
		response := performJSONRequest(t, router, stdhttp.MethodGet, path, resetAdmin.Token, nil)
		if response.Code != stdhttp.StatusForbidden {
			t.Fatalf("forced-change %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func performJSONRequestFrom(t *testing.T, handler stdhttp.Handler, method, path, remoteAddr, cloudflareIP string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, &body)
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("CF-Connecting-IP", cloudflareIP)
	request.Header.Set(deviceIDHeader, "test-browser-device")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestWeakInitialPasswordIsRejected(t *testing.T) {
	appStore, _ := newHTTPTestStore(t)
	defer appStore.Close()
	err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "weak-member", RealName: "弱密码成员", Role: "USER", InitialPassword: "weak",
	})
	if err == nil || !strings.Contains(err.Error(), config.ErrWeakPassword.Error()) {
		t.Fatalf("weak initial password error=%v", err)
	}
}
