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

func TestDeleteLaborConvertFinanceFileHTTPStatuses(t *testing.T) {
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
		Username: "finance-member", RealName: "财务成员", Role: "USER", InitialPassword: "password",
	}); err != nil {
		t.Fatal(err)
	}
	saveBatch := func() types.FinanceLocalBatch {
		t.Helper()
		response, err := appStore.SaveFinanceExportsLocal(types.FinanceSaveLocalRequest{
			StartDate: "2026-08-01", EndDate: "2026-08-18",
		})
		if err != nil {
			t.Fatal(err)
		}
		return response.Batch
	}

	unused := saveBatch()
	deleted := performJSONRequest(t, router, stdhttp.MethodDelete, "/api/labor-convert/finance-files/"+unused.ID, login.Token, nil)
	if deleted.Code != stdhttp.StatusOK {
		t.Fatalf("unused batch delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	missing := performJSONRequest(t, router, stdhttp.MethodDelete, "/api/labor-convert/finance-files/"+unused.ID, login.Token, nil)
	if missing.Code != stdhttp.StatusNotFound {
		t.Fatalf("missing batch delete status=%d body=%s", missing.Code, missing.Body.String())
	}

	referenced := saveBatch()
	if _, err := appStore.ConvertLaborFinanceBatch(referenced.ID, 5000); err != nil {
		t.Fatal(err)
	}
	conflict := performJSONRequest(t, router, stdhttp.MethodDelete, "/api/labor-convert/finance-files/"+referenced.ID, login.Token, nil)
	if conflict.Code != stdhttp.StatusConflict {
		t.Fatalf("referenced batch delete status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	history, err := appStore.ListLaborConversionRuns()
	if err != nil || len(history) != 1 {
		t.Fatalf("labor history changed after conflict: items=%d err=%v", len(history), err)
	}

	archived := saveBatch()
	if err := appStore.SetSemesterArchived(appStore.ActiveSemester().ID, true); err != nil {
		t.Fatal(err)
	}
	locked := performJSONRequest(t, router, stdhttp.MethodDelete, "/api/labor-convert/finance-files/"+archived.ID, login.Token, nil)
	if locked.Code != stdhttp.StatusLocked {
		t.Fatalf("archived batch delete status=%d body=%s", locked.Code, locked.Body.String())
	}
}
