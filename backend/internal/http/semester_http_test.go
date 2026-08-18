package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/golang-jwt/jwt/v5"
)

func TestSemesterHTTPHotSwitchArchiveGuardAndSessionVersion(t *testing.T) {
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

	metaBefore := performJSONRequest(t, router, stdhttp.MethodGet, "/api/meta/config", login.Token, nil)
	if metaBefore.Code != stdhttp.StatusOK || metaBefore.Header().Get("X-DMS-Context-Version") == "" {
		t.Fatalf("meta before switch status=%d headers=%v", metaBefore.Code, metaBefore.Header())
	}

	createResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/semesters", login.Token, map[string]any{
		"name":        "http-next",
		"firstMonday": "20260907",
		"cloneFromId": appStore.ActiveSemester().ID,
	})
	if createResponse.Code != stdhttp.StatusCreated {
		t.Fatalf("create semester status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created types.SemesterSummary
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	activateResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/semesters/"+created.ID+"/activate", login.Token, nil)
	if activateResponse.Code != stdhttp.StatusOK {
		t.Fatalf("activate status=%d body=%s", activateResponse.Code, activateResponse.Body.String())
	}
	if activateResponse.Header().Get("X-DMS-Semester-ID") != created.ID || activateResponse.Header().Get("X-DMS-Context-Version") == "" {
		t.Fatalf("activation context headers missing: %v", activateResponse.Header())
	}
	meAfterSwitch := performJSONRequest(t, router, stdhttp.MethodGet, "/api/auth/me", login.Token, nil)
	if meAfterSwitch.Code != stdhttp.StatusOK {
		t.Fatalf("existing JWT did not survive semester switch: status=%d body=%s", meAfterSwitch.Code, meAfterSwitch.Body.String())
	}

	archiveResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/semesters/"+created.ID+"/archive", login.Token, nil)
	if archiveResponse.Code != stdhttp.StatusOK {
		t.Fatalf("archive status=%d body=%s", archiveResponse.Code, archiveResponse.Body.String())
	}
	settingsWrite := performJSONRequest(t, router, stdhttp.MethodPut, "/api/system-settings", login.Token, map[string]any{
		"firstMonday":      "20260907",
		"laborSeed":        "42",
		"workStudyContent": "测试内容",
	})
	if settingsWrite.Code != stdhttp.StatusLocked {
		t.Fatalf("archived semester write status=%d body=%s", settingsWrite.Code, settingsWrite.Body.String())
	}
	templateWrite := performJSONRequest(t, router, stdhttp.MethodPut, "/api/templates/global", login.Token, nil)
	if templateWrite.Code == stdhttp.StatusLocked {
		t.Fatal("global template operation was incorrectly blocked by archived semester guard")
	}

	oldClaims := middleware.Claims{
		UserID: login.User.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	oldToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, oldClaims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	oldTokenResponse := performJSONRequest(t, router, stdhttp.MethodGet, "/api/auth/me", oldToken, nil)
	if oldTokenResponse.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("legacy JWT without session version was accepted: status=%d", oldTokenResponse.Code)
	}
}

func newHTTPTestStore(t *testing.T) (*store.Store, config.AppConfig) {
	t.Helper()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "backend", ".env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("APP_PORT=3000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{
		Port:                 "3000",
		ControlDatabasePath:  filepath.Join(dir, "data", "control.db"),
		SemesterDatabaseDir:  filepath.Join(dir, "data", "semesters"),
		DatabasePath:         filepath.Join(dir, "data", "personnel.db"),
		PrivateMembersPath:   filepath.Join(dir, "data", "member.json"),
		JWTSecret:            "http-test-secret",
		AdminPassword:        "admin-password",
		FirstMonday:          "20260302",
		EnvFilePath:          envPath,
		WorkStudyTemplateDir: filepath.Join(dir, "templates"),
		WorkStudyContent:     "测试工作",
	}
	appStore, err := store.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return appStore, cfg
}

func performJSONRequest(t *testing.T, handler stdhttp.Handler, method, path, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
