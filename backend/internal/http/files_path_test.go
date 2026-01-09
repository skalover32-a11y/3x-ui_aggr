package httpapi

import "testing"

func TestNormalizePath(t *testing.T) {
	_, err := normalizePath("../etc/passwd")
	if err == nil {
		t.Fatalf("expected traversal to fail")
	}
	_, err = normalizePath("var/log")
	if err == nil {
		t.Fatalf("expected relative path to fail")
	}
	got, err := normalizePath("/var/log/nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/var/log/nginx" {
		t.Fatalf("expected cleaned path, got %s", got)
	}
}

func TestIsPathAllowed(t *testing.T) {
	roots := []string{"/opt", "/var/log", "/home/*/backups"}
	if !isPathAllowed("/opt/app/config.yaml", roots) {
		t.Fatalf("expected /opt to be allowed")
	}
	if !isPathAllowed("/home/user/backups/2024/db.sql", roots) {
		t.Fatalf("expected wildcard backups to be allowed")
	}
	if isPathAllowed("/home/user/private", roots) {
		t.Fatalf("expected /home/user/private to be blocked")
	}
}
