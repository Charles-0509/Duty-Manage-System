package http

import (
	"encoding/json"
	stdhttp "net/http"
	"slices"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestDashboardViewerCannotAccessOtherAPIs(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "public", RealName: "只读访客", Role: "VIEWER", InitialPassword: "admin@",
	}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(cfg, appStore)
	response := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "public", "password": "admin@",
	})
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("viewer login status=%d body=%s", response.Code, response.Body.String())
	}
	var login types.LoginResponse
	if err := json.Unmarshal(response.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if login.User.Role != "VIEWER" || login.User.MustChangePassword || !slices.Equal(login.User.Permissions, []string{"view_dashboard"}) {
		t.Fatalf("unexpected viewer permissions: %+v", login.User)
	}
	for _, path := range []string{"/api/dashboard", "/api/meta/config", "/api/auth/me"} {
		response := performJSONRequest(t, router, stdhttp.MethodGet, path, login.Token, nil)
		if response.Code != stdhttp.StatusOK {
			t.Fatalf("viewer read %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	refreshed := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/refresh", "", map[string]any{"refreshToken": login.RefreshToken})
	if refreshed.Code != stdhttp.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	var next types.LoginResponse
	if err := json.Unmarshal(refreshed.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.User.Role != "VIEWER" {
		t.Fatalf("refresh changed viewer role: %+v", next.User)
	}
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") {
			continue
		}
		if route.Method == stdhttp.MethodGet && slices.Contains([]string{"/api/dashboard", "/api/meta/config", "/api/auth/me"}, route.Path) ||
			route.Method == stdhttp.MethodPost && slices.Contains([]string{"/api/auth/login", "/api/auth/refresh", "/api/auth/logout"}, route.Path) {
			continue
		}
		path := strings.NewReplacer(":versionId", "version", ":username", "admin", ":week", "1", ":id", "1").Replace(route.Path)
		t.Run(route.Method+" "+path, func(t *testing.T) {
			response := performJSONRequest(t, router, route.Method, path, next.Token, map[string]any{})
			if response.Code != stdhttp.StatusForbidden && response.Code != stdhttp.StatusUnauthorized {
				t.Fatalf("viewer accessed %s %s: status=%d body=%s", route.Method, path, response.Code, response.Body.String())
			}
		})
	}
	logout := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/logout", next.Token, map[string]any{"refreshToken": next.RefreshToken})
	if logout.Code != stdhttp.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
}
