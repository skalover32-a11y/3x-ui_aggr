package alerts

import "testing"

func TestResourceAlertsHaveStricterRecoveryWindow(t *testing.T) {
	svc := &Service{policy: defaultPolicy()}

	if got := svc.defaultRecoverAfterOK(AlertMemory); got != 6 {
		t.Fatalf("expected memory recover_after_ok=6, got %d", got)
	}
	if got := svc.defaultRecoverAfterOK(AlertCPU); got != 6 {
		t.Fatalf("expected cpu recover_after_ok=6, got %d", got)
	}
	if got := svc.defaultRecoverAfterOK(AlertDisk); got != 6 {
		t.Fatalf("expected disk recover_after_ok=6, got %d", got)
	}
	if got := svc.defaultRecoverAfterOK(AlertConnection); got != 3 {
		t.Fatalf("expected connection recover_after_ok=3, got %d", got)
	}
}

func TestResourceAlertsUseMinConsecutiveFailsPolicy(t *testing.T) {
	svc := &Service{policy: defaultPolicy()}

	if got := svc.minConsecutiveFails(AlertMemory); got != 2 {
		t.Fatalf("expected memory min_consecutive_fails=2, got %d", got)
	}
	if got := svc.minConsecutiveFails(AlertCPU); got != 2 {
		t.Fatalf("expected cpu min_consecutive_fails=2, got %d", got)
	}
	if got := svc.minConsecutiveFails(AlertDisk); got != 2 {
		t.Fatalf("expected disk min_consecutive_fails=2, got %d", got)
	}
}

