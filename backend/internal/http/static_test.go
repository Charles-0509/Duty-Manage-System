package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMissingFrontendAssetReturnsNotFound(t *testing.T) {
	router := newFrontendTestRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/missing-page-module.js", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "<!doctype html") {
		t.Fatal("missing asset unexpectedly returned the SPA index")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
}

func TestFrontendIndexDisablesCaching(t *testing.T) {
	router := newFrontendTestRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
}

func TestFrontendRouteStillFallsBackToIndex(t *testing.T) {
	router := newFrontendTestRouter()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/system-settings", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(strings.ToLower(recorder.Body.String()), "<!doctype html") {
		t.Fatal("client route did not return the SPA index")
	}
}

func newFrontendTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerFrontendRoutes(router)
	return router
}
