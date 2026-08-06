package http

import (
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"personnel-management-go/internal/types"

	"github.com/google/uuid"
)

func TestDeleteLaborConvertHistoryHTTPNotFoundAndArchiveGuard(t *testing.T) {
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

	missingPath := "/api/labor-convert/history/" + uuid.NewString()
	missing := performJSONRequest(t, router, stdhttp.MethodDelete, missingPath, login.Token, nil)
	if missing.Code != stdhttp.StatusNotFound {
		t.Fatalf("missing history delete status=%d body=%s", missing.Code, missing.Body.String())
	}

	if err := appStore.SetSemesterArchived(appStore.ActiveSemester().ID, true); err != nil {
		t.Fatal(err)
	}
	locked := performJSONRequest(t, router, stdhttp.MethodDelete, missingPath, login.Token, nil)
	if locked.Code != stdhttp.StatusLocked {
		t.Fatalf("archived history delete status=%d body=%s", locked.Code, locked.Body.String())
	}
}
