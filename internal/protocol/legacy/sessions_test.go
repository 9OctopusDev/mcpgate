package legacy

import (
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func TestSessionsBindPrincipalExpireAndCap(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	sessions := NewSessions(1, 15*time.Minute, func() time.Time { return now })
	principal := contracts.Principal{Issuer: "issuer", Subject: "subject", Tenant: "tenant", ClientID: "client", PartitionDigest: "partition"}
	session, err := sessions.Create("server", principal, map[string]bool{"tools": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create("server", principal, nil); err == nil {
		t.Fatal("session cap not enforced")
	}
	if _, ok := sessions.Get(session.ID, "server", contracts.Principal{PartitionDigest: "other"}); ok {
		t.Fatal("cross-principal session accepted")
	}
	now = now.Add(16 * time.Minute)
	if _, ok := sessions.Get(session.ID, "server", principal); ok {
		t.Fatal("expired session accepted")
	}
}

func TestCompatibleToolsOmitNonObjectStructuredOutput(t *testing.T) {
	tools := CompatibleTools([]contracts.ToolContract{
		{Name: "object", OutputSchema: []byte(`{"type":"object"}`)},
		{Name: "array", OutputSchema: []byte(`{"type":"array"}`)},
		{Name: "text"},
	})
	if len(tools) != 2 || tools[0].Name != "object" || tools[1].Name != "text" {
		t.Fatalf("tools=%+v", tools)
	}
}
