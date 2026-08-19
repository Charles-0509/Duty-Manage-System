package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestAutoScheduleHTTPPreservesSubmittedSchedule(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	loginResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	if loginResponse.Code != stdhttp.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	var login types.LoginResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}

	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "locked", RealName: "锁定成员", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatal(err)
	}

	response := performJSONRequest(t, router, stdhttp.MethodPost, "/api/schedule/auto-generate", login.Token, map[string]any{
		"perSlot": 1,
		"schedule": map[string][]string{
			"Mon-1": {"锁定成员(单)", "锁定成员(双)"},
		},
	})
	if response.Code != stdhttp.StatusOK {
		t.Fatalf("auto schedule status=%d body=%s", response.Code, response.Body.String())
	}
	var result types.AutoScheduleResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.Schedule["Mon-1"], ","), "锁定成员(单双)") {
		t.Fatalf("Mon-1 assignments = %v, want submitted locked member", result.Schedule["Mon-1"])
	}
}
