package agentfs

import "testing"

func TestNormalizePathRejectsInvalid(t *testing.T) {
	cases := []string{
		"",
		"etc",
		"../etc",
		"/../etc",
		"/etc/../..",
		"/etc/\x00/passwd",
		"\\etc\\passwd",
		"/var/..",
	}
	for _, raw := range cases {
		if _, err := NormalizePath(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestNormalizePathAcceptsAbsolute(t *testing.T) {
	out, err := NormalizePath("/etc/nginx/nginx.conf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/etc/nginx/nginx.conf" {
		t.Fatalf("unexpected path: %s", out)
	}
}

func TestBlockedPrefixes(t *testing.T) {
	blocked := []string{"/proc", "/proc/cpuinfo", "/sys", "/sys/kernel", "/dev", "/dev/null"}
	for _, p := range blocked {
		if !IsBlocked(p) {
			t.Fatalf("expected blocked for %q", p)
		}
	}
	ok := []string{"/etc", "/var/log", "/home/user"}
	for _, p := range ok {
		if IsBlocked(p) {
			t.Fatalf("expected allowed for %q", p)
		}
	}
}
