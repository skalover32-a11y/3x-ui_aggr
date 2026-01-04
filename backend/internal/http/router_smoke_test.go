package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterInit(t *testing.T) {
	h := &Handler{JWTSecret: []byte("test")}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("router init panic: %v", r)
		}
	}()
	r := NewRouter(h)
	resp := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/healthz", nil)
	r.ServeHTTP(resp, req)
	if resp.Code == http.StatusNotFound {
		t.Fatalf("expected /api/healthz to be registered")
	}
	resp = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/services", nil)
	r.ServeHTTP(resp, req)
	if resp.Code == http.StatusNotFound {
		t.Fatalf("expected /api/services to be registered")
	}
}
