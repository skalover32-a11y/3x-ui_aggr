package httpapi

import (
	"testing"

	"agr_3x_ui/internal/db"
)

func TestNormalizeNodeKind(t *testing.T) {
	kind, err := normalizeNodeKind("")
	if err != nil || kind != "PANEL" {
		t.Fatalf("expected default PANEL, got %q err=%v", kind, err)
	}
	kind, err = normalizeNodeKind("host")
	if err != nil || kind != "HOST" {
		t.Fatalf("expected HOST, got %q err=%v", kind, err)
	}
	if _, err := normalizeNodeKind("other"); err == nil {
		t.Fatalf("expected error for invalid kind")
	}
}

func TestValidateNodeCreatePanel(t *testing.T) {
	req := &nodeCreateRequest{
		Name:          "node-1",
		BaseURL:       "",
		PanelUsername: "",
		PanelPassword: "",
	}
	if err := validateNodeCreate("PANEL", req); err == nil {
		t.Fatalf("expected error for missing panel fields")
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

func TestValidateNodeUpdatePanel(t *testing.T) {
	node := &db.Node{
		BaseURL:       "https://example.com",
		PanelUsername: "admin",
	}
	req := &nodeUpdateRequest{
		BaseURL:       strPtr(""),
		PanelUsername: strPtr(""),
	}
	if err := validateNodeUpdate("PANEL", node, req); err == nil {
		t.Fatalf("expected error for empty panel fields")
	}
}

func strPtr(value string) *string {
	v := value
	return &v
}
