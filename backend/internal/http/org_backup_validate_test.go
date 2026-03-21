package httpapi

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateOrgBackupPayloadSkipsBrokenReferences(t *testing.T) {
	nodeID := uuid.New()
	serviceID := uuid.New()
	botID := uuid.New()

	payload := orgBackupPayload{
		Nodes: []orgBackupNode{
			{ID: nodeID, Name: "node-1"},
		},
		Services: []orgBackupService{
			{ID: serviceID, NodeID: uuidPtr(nodeID), Kind: "HTTP"},
			{ID: uuid.New(), NodeID: uuidPtr(uuid.New()), Kind: "HTTP"},
			{ID: uuid.New(), Name: "external-ftp", Kind: "CUSTOM_FTP"},
		},
		Bots: []orgBackupBot{
			{ID: botID, NodeID: nodeID, Name: "bot-1", Kind: "HTTP"},
		},
		Checks: []orgBackupCheck{
			{ID: uuid.New(), TargetType: "service", TargetID: serviceID, Type: "HTTP"},
			{ID: uuid.New(), TargetType: "service", TargetID: uuid.New(), Type: "HTTP"},
			{ID: uuid.New(), TargetType: "unknown", TargetID: uuid.New(), Type: "HTTP"},
		},
		Keys: []orgBackupKey{
			{ID: uuid.New(), Filename: "id_rsa", Ext: "pem", ContentEnc: "x", SizeBytes: 1, NodeID: &nodeID},
			{ID: uuid.New(), Filename: "id2_rsa", Ext: "pem", ContentEnc: "x", SizeBytes: 1, NodeID: uuidPtr(uuid.New())},
		},
	}

	valid, skipped, warnings := validateOrgBackupPayload(payload)
	if got := len(valid.Nodes); got != 1 {
		t.Fatalf("valid nodes=%d want=1", got)
	}
	if got := len(valid.Services); got != 2 {
		t.Fatalf("valid services=%d want=2", got)
	}
	if got := len(valid.Bots); got != 1 {
		t.Fatalf("valid bots=%d want=1", got)
	}
	if got := len(valid.Checks); got != 1 {
		t.Fatalf("valid checks=%d want=1", got)
	}
	if got := len(valid.Keys); got != 2 {
		t.Fatalf("valid keys=%d want=2", got)
	}
	if valid.Keys[1].NodeID != nil {
		t.Fatalf("expected broken key node mapping to be cleared")
	}
	if skipped.Services != 1 {
		t.Fatalf("skipped services=%d want=1", skipped.Services)
	}
	if skipped.Checks != 2 {
		t.Fatalf("skipped checks=%d want=2", skipped.Checks)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected validation warnings")
	}
}

func TestBackupCounts(t *testing.T) {
	payload := orgBackupPayload{
		Nodes:    []orgBackupNode{{ID: uuid.New()}},
		Services: []orgBackupService{{ID: uuid.New()}},
		Bots:     []orgBackupBot{{ID: uuid.New()}},
		Checks:   []orgBackupCheck{{ID: uuid.New()}},
		Keys:     []orgBackupKey{{ID: uuid.New()}},
	}
	counts := backupCounts(payload)
	if counts.Nodes != 1 || counts.Services != 1 || counts.Bots != 1 || counts.Checks != 1 || counts.Keys != 1 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
	if counts.Empty() {
		t.Fatalf("expected non-empty counts")
	}
}

func uuidPtr(v uuid.UUID) *uuid.UUID {
	return &v
}
