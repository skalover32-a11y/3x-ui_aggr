package ops

import "testing"

func TestJobStatusFromFailures(t *testing.T) {
	status, errMsg := jobStatusFromFailures(0)
	if status != JobSuccess {
		t.Fatalf("expected %s, got %s", JobSuccess, status)
	}
	if errMsg != nil {
		t.Fatal("expected nil error message on success")
	}

	status, errMsg = jobStatusFromFailures(2)
	if status != JobFailed {
		t.Fatalf("expected %s, got %s", JobFailed, status)
	}
	if errMsg == nil || *errMsg == "" {
		t.Fatal("expected error message on failure")
	}
}
