package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"personnel-management-go/internal/types"
)

func TestPasswordPolicyAcrossCreateChangeAndReset(t *testing.T) {
	s, cfg := newHTTPTestStore(t)
	defer s.Close()
	router := NewRouter(cfg, s)
	login := performJSONRequest(t, router, http.MethodPost, "/api/auth/login", "", types.LoginRequest{Username: "admin", Password: "admin-password"})
	var admin types.LoginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &admin); err != nil || login.Code != http.StatusOK {
		t.Fatalf("admin login: %s", login.Body.String())
	}
	for _, role := range []string{"USER", "VIEWER"} {
		t.Run(role, func(t *testing.T) {
			username := "password-" + role
			for _, password := range []string{"Ab12!", "abcdefghijkl"} {
				response := performJSONRequest(t, router, http.MethodPost, "/api/users", admin.Token, types.CreateMemberRequest{Username: username, RealName: username, Role: role, InitialPassword: password})
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "密码不符合安全要求") {
					t.Fatalf("invalid create: %d %s", response.Code, response.Body.String())
				}
			}
			created := performJSONRequest(t, router, http.MethodPost, "/api/users", admin.Token, types.CreateMemberRequest{Username: username, RealName: username, Role: role, InitialPassword: "abc123"})
			if created.Code != http.StatusCreated {
				t.Fatalf("6-character password rejected: %s", created.Body.String())
			}
			user, err := s.GetUserByUsername(username)
			if err != nil {
				t.Fatal(err)
			}
			memberLogin := performJSONRequest(t, router, http.MethodPost, "/api/auth/login", "", types.LoginRequest{Username: username, Password: "abc123"})
			var member types.LoginResponse
			if err := json.Unmarshal(memberLogin.Body.Bytes(), &member); err != nil || memberLogin.Code != http.StatusOK {
				t.Fatalf("member login: %s", memberLogin.Body.String())
			}
			if role == "USER" {
				for _, password := range []string{"Ab12!", "abcdefghijkl"} {
					response := performJSONRequest(t, router, http.MethodPut, "/api/auth/password", member.Token, types.ChangePasswordRequest{CurrentPassword: "abc123", NewPassword: password})
					if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "密码不符合安全要求") {
						t.Fatalf("invalid change: %d %s", response.Code, response.Body.String())
					}
				}
				response := performJSONRequest(t, router, http.MethodPut, "/api/auth/password", member.Token, types.ChangePasswordRequest{CurrentPassword: "abc123", NewPassword: "Abcdef"})
				if response.Code != http.StatusOK {
					t.Fatalf("valid change: %s", response.Body.String())
				}
			}
			resetURL := "/api/users/" + strconv.FormatInt(user.ID, 10) + "/password"
			response := performJSONRequest(t, router, http.MethodPatch, resetURL, admin.Token, map[string]string{"newPassword": "abcdefghijkl"})
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "至少两种") {
				t.Fatalf("invalid reset: %d %s", response.Code, response.Body.String())
			}
			response = performJSONRequest(t, router, http.MethodPatch, resetURL, admin.Token, map[string]string{"newPassword": "12345!"})
			if response.Code != http.StatusOK {
				t.Fatalf("valid reset: %s", response.Body.String())
			}
			if _, err := s.Authenticate(username, "12345!"); err != nil {
				t.Fatalf("reset password unusable: %v", err)
			}
		})
	}
}
