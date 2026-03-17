package ops

import "testing"

func TestChooseVLFProtoCommandPlan(t *testing.T) {
	base := InstallVLFProtoParams{
		RepoURL:    "https://github.com/skalover32-a11y/VLF-Proto.git",
		Ref:        "main",
		GoVersion:  "1.25.1",
		InstallDir: "/opt/vlf-proto",
	}

	t.Run("fresh install", func(t *testing.T) {
		plan := chooseVLFProtoCommandPlan(base, vlfProtoInstallState{})
		if plan.Mode != "install" {
			t.Fatalf("expected install mode, got %q", plan.Mode)
		}
		if plan.Command == "" {
			t.Fatal("expected install command")
		}
	})

	t.Run("existing update script uses update", func(t *testing.T) {
		plan := chooseVLFProtoCommandPlan(base, vlfProtoInstallState{
			HasUpdateScript:    true,
			HasExistingInstall: true,
		})
		if plan.Mode != "update" {
			t.Fatalf("expected update mode, got %q", plan.Mode)
		}
		if plan.Command == "" {
			t.Fatal("expected update command")
		}
	})

	t.Run("legacy install falls back to forced reinstall", func(t *testing.T) {
		plan := chooseVLFProtoCommandPlan(base, vlfProtoInstallState{
			HasExistingInstall: true,
		})
		if plan.Mode != "reinstall" {
			t.Fatalf("expected reinstall mode, got %q", plan.Mode)
		}
		if plan.Command == "" {
			t.Fatal("expected reinstall command")
		}
		if plan.LogLine == "" {
			t.Fatal("expected reinstall explanation")
		}
	})

	t.Run("force flag wins over update path", func(t *testing.T) {
		forced := base
		forced.Force = true
		plan := chooseVLFProtoCommandPlan(forced, vlfProtoInstallState{
			HasUpdateScript:    true,
			HasExistingInstall: true,
		})
		if plan.Mode != "reinstall" {
			t.Fatalf("expected reinstall mode, got %q", plan.Mode)
		}
	})
}
