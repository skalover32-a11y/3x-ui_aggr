package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"agr_3x_ui/internal/db"
)

func TestEnsureAgentBinaryMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod/exec bit not reliable on windows")
	}
	svc := &Service{AgentBinary: filepath.Join(t.TempDir(), "missing")}
	_, _, err := svc.ensureAgentBinary()
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "rebuild backend image") {
		t.Fatalf("expected rebuild hint, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected path in error, got: %s", err.Error())
	}
}

func TestEnsureAgentBinaryOK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod/exec bit not reliable on windows")
	}
	temp := filepath.Join(t.TempDir(), "vlf-agent")
	if err := os.WriteFile(temp, []byte("hello"), 0755); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	svc := &Service{AgentBinary: temp}
	path, logLine, err := svc.ensureAgentBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != temp {
		t.Fatalf("expected path %s, got %s", temp, path)
	}
	if !strings.Contains(logLine, "sha256=") {
		t.Fatalf("expected sha256 in log, got %s", logLine)
	}
}

func TestBuildDeployParamsUsesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod/exec bit not reliable on windows")
	}
	repo := t.TempDir()
	serviceDir := filepath.Join(repo, "deploy", "agent")
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "vlf-agent.service"), []byte("[Unit]\n"), 0644); err != nil {
		t.Fatalf("write service: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "vlf-agent")
	if err := os.WriteFile(bin, []byte("hello"), 0755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	svc := &Service{
		AgentBinary:   bin,
		AllowCIDR:     "1.2.3.4/32",
		SudoPasswords: []string{"pass"},
		RepoPath:      repo,
	}
	payload, _ := json.Marshal(map[string]any{
		"agent_port": 9191,
	})
	params, err := svc.buildDeployParams(nil, &db.Node{}, datatypes.JSON(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.BinaryPath != bin {
		t.Fatalf("expected binary path %s, got %s", bin, params.BinaryPath)
	}
	if !strings.Contains(params.PreLog, "agent binary") || !strings.Contains(params.PreLog, "service template") {
		t.Fatalf("expected prelog details, got %s", params.PreLog)
	}
}
