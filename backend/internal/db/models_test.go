package db

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestNodeAgentInsecureColumn(t *testing.T) {
	s, err := schema.Parse(&Node{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema parse failed: %v", err)
	}
	field, ok := s.FieldsByName["AgentInsecureTLS"]
	if !ok {
		t.Fatal("AgentInsecureTLS field missing")
	}
	if field.DBName != "agent_allow_insecure_tls" {
		t.Fatalf("expected column agent_allow_insecure_tls, got %s", field.DBName)
	}
}
