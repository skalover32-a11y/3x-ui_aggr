package ops

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatchRemnaComposeVolumesIdempotent(t *testing.T) {
	input := []byte(`version: "3.9"
services:
  remnanode:
    image: remnawave/node:latest
    volumes:
      - /opt/remnanode/data:/var/lib/remnanode
`)
	params := normalizeRemnaParams(RemnaGeodataParams{})

	updated, changed, err := patchRemnaComposeVolumes(input, params)
	if err != nil {
		t.Fatalf("patch compose: %v", err)
	}
	if !changed {
		t.Fatalf("expected compose to change on first patch")
	}

	updatedAgain, changedAgain, err := patchRemnaComposeVolumes(updated, params)
	if err != nil {
		t.Fatalf("patch compose second pass: %v", err)
	}
	if changedAgain {
		t.Fatalf("expected idempotent compose patch")
	}

	text := string(updatedAgain)
	if strings.Count(text, "/usr/local/share/xray/geosite.dat") != 1 {
		t.Fatalf("expected one geosite mount, got:\n%s", text)
	}
	if strings.Count(text, "/usr/local/share/xray/geoip.dat") != 1 {
		t.Fatalf("expected one geoip mount, got:\n%s", text)
	}
}

func TestPatchRemnaComposeVolumesFindsServiceByContainerName(t *testing.T) {
	input := []byte(`services:
  xray:
    container_name: remnanode
    image: remnawave/node:latest
`)
	params := normalizeRemnaParams(RemnaGeodataParams{})

	updated, changed, err := patchRemnaComposeVolumes(input, params)
	if err != nil {
		t.Fatalf("patch compose: %v", err)
	}
	if !changed {
		t.Fatalf("expected compose to change")
	}

	var root map[string]any
	if err := yaml.Unmarshal(updated, &root); err != nil {
		t.Fatalf("parse updated yaml: %v", err)
	}
	services := root["services"].(map[string]any)
	xray := services["xray"].(map[string]any)
	volumes := xray["volumes"].([]any)
	if len(volumes) != 2 {
		t.Fatalf("expected exactly 2 mounts, got %d", len(volumes))
	}
}
