package httpapi

import "testing"

func TestCanSSHRole(t *testing.T) {
	if !canSSHRole("admin") {
		t.Fatalf("admin should be allowed")
	}
	if canSSHRole("operator") {
		t.Fatalf("operator should not be allowed")
	}
	if canSSHRole("viewer") {
		t.Fatalf("viewer should not be allowed")
	}
}
