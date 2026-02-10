package httpapi

import (
	"testing"

	"agr_3x_ui/internal/db"
)

func TestNormalizeNodeKind(t *testing.T) {
	kind, err := normalizeNodeKind("")
	if err != nil || kind != "SERVER" {
		t.Fatalf("expected default SERVER, got %q err=%v", kind, err)
	}
	kind, err = normalizeNodeKind("host")
	if err != nil || kind != "HOST" {
		t.Fatalf("expected HOST, got %q err=%v", kind, err)
	}
	kind, err = normalizeNodeKind("panel")
	if err != nil || kind != "SERVER" {
		t.Fatalf("expected PANEL alias to map to SERVER, got %q err=%v", kind, err)
	}
	if _, err := normalizeNodeKind("other"); err == nil {
		t.Fatalf("expected error for invalid kind")
	}
}

func TestValidateNodeCreateServer(t *testing.T) {
	req := &nodeCreateRequest{
		Name:          "node-1",
		BaseURL:       "",
		PanelUsername: "",
		PanelPassword: "",
	}
	if err := validateNodeCreate("SERVER", req); err != nil {
		t.Fatalf("expected no error for server node, got %v", err)
	}
}

func TestValidateNodeCreateHost(t *testing.T) {
	req := &nodeCreateRequest{
		Name:          "node-1",
		BaseURL:       "",
		PanelUsername: "",
		PanelPassword: "",
	}
	if err := validateNodeCreate("HOST", req); err != nil {
		t.Fatalf("expected no error for host node, got %v", err)
	}
}

func TestValidateNodeUpdateServer(t *testing.T) {
	node := &db.Node{
		BaseURL:       "https://example.com",
		PanelUsername: "admin",
	}
	req := &nodeUpdateRequest{
		BaseURL:       strPtr(""),
		PanelUsername: strPtr(""),
	}
	if err := validateNodeUpdate("SERVER", node, req); err != nil {
		t.Fatalf("expected no error for update, got %v", err)
	}
}

func strPtr(value string) *string {
	v := value
	return &v
}
