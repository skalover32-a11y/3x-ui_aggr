package sshws

import "testing"

func TestManagerLimit(t *testing.T) {
	mgr := NewManager(2)
	release1, err := mgr.TryAcquire()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	release2, err := mgr.TryAcquire()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := mgr.TryAcquire(); err == nil {
		t.Fatalf("expected limit error")
	}
	if mgr.Active() != 2 {
		t.Fatalf("expected active=2, got %d", mgr.Active())
	}
	release1()
	if mgr.Active() != 1 {
		t.Fatalf("expected active=1, got %d", mgr.Active())
	}
	release2()
	if mgr.Active() != 0 {
		t.Fatalf("expected active=0, got %d", mgr.Active())
	}
}
