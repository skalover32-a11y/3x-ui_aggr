package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib/pq"

	"agr_3x_ui/internal/db"
)

func TestExecuteServiceCheckHTTPExpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/404" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	worker := &Worker{}
	node := &db.Node{VerifyTLS: true}
	check := &db.Check{Type: "HTTP", TimeoutMS: 1000, Retries: 0}
	service := &db.Service{
		Kind:           "CUSTOM_HTTP",
		URL:            stringPtr(srv.URL),
		HealthPath:     stringPtr("/"),
		ExpectedStatus: pq.Int64Array{200},
		IsEnabled:      true,
	}

	ok, _, statusCode, _, _ := worker.executeServiceCheck(context.Background(), node, service, check)
	if !ok || statusCode != http.StatusOK {
		t.Fatalf("expected ok status 200, got ok=%v status=%d", ok, statusCode)
	}

	service.HealthPath = stringPtr("/404")
	ok, _, statusCode, _, _ = worker.executeServiceCheck(context.Background(), node, service, check)
	if ok || statusCode != http.StatusNotFound {
		t.Fatalf("expected fail status 404, got ok=%v status=%d", ok, statusCode)
	}
}

func stringPtr(value string) *string {
	v := value
	return &v
}
