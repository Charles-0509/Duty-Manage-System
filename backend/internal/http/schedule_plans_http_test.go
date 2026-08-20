package http

import (
	"encoding/json"
	stdhttp "net/http"
	"testing"

	"personnel-management-go/internal/types"
)

func TestSchedulePlanHTTPPublishesOnePlanAndRejectsArchivedWrites(t *testing.T) {
	appStore, cfg := newHTTPTestStore(t)
	defer appStore.Close()
	router := NewRouter(cfg, appStore)

	loginResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin",
		"password": "admin-password",
	})
	var login types.LoginResponse
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "schedule-http", RealName: "接口排班成员", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatal(err)
	}

	createdResponse := performJSONRequest(t, router, stdhttp.MethodPost, "/api/schedule-plans", login.Token, map[string]any{
		"name":     "接口排班表",
		"schedule": map[string][]string{"Mon-1": {"接口排班成员(单双)"}},
	})
	if createdResponse.Code != stdhttp.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created types.SchedulePlanSummary
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.IsPublished {
		t.Fatal("new plan was published")
	}

	unpublished := performJSONRequest(t, router, stdhttp.MethodGet, "/api/schedule", login.Token, nil)
	var unpublishedSchedule types.ScheduleResponse
	if err := json.Unmarshal(unpublished.Body.Bytes(), &unpublishedSchedule); err != nil {
		t.Fatal(err)
	}
	if len(unpublishedSchedule.Schedule) != 0 {
		t.Fatalf("unpublished schedule leaked: %+v", unpublishedSchedule.Schedule)
	}

	published := performJSONRequest(t, router, stdhttp.MethodPost, "/api/schedule-plans/"+created.ID+"/publish", login.Token, nil)
	if published.Code != stdhttp.StatusOK {
		t.Fatalf("publish status=%d body=%s", published.Code, published.Body.String())
	}
	visible := performJSONRequest(t, router, stdhttp.MethodGet, "/api/schedule", login.Token, nil)
	var visibleSchedule types.ScheduleResponse
	if err := json.Unmarshal(visible.Body.Bytes(), &visibleSchedule); err != nil {
		t.Fatal(err)
	}
	if len(visibleSchedule.Schedule["Mon-1"]) != 1 {
		t.Fatalf("visible schedule=%+v", visibleSchedule.Schedule)
	}

	legacyWrite := performJSONRequest(t, router, stdhttp.MethodPut, "/api/schedule", login.Token, map[string]any{"schedule": map[string]any{}})
	if legacyWrite.Code != stdhttp.StatusNotFound {
		t.Fatalf("legacy write status=%d", legacyWrite.Code)
	}

	active := appStore.ActiveSemester()
	if err := appStore.SetSemesterArchived(active.ID, true); err != nil {
		t.Fatal(err)
	}
	locked := performJSONRequest(t, router, stdhttp.MethodPost, "/api/schedule-plans", login.Token, map[string]any{
		"name":     "归档写入",
		"schedule": map[string]any{},
	})
	if locked.Code != stdhttp.StatusLocked {
		t.Fatalf("locked status=%d body=%s", locked.Code, locked.Body.String())
	}
}
