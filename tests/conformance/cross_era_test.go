package conformance_test

import (
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/protocol/legacy"
	"github.com/9OctopusDev/mcpgate/internal/protocol/modern"
)

func TestCrossEraProtocolFacadesRemainDistinct(t *testing.T) {
	if legacy.ProtocolVersion == modern.ProtocolVersion {
		t.Fatal("legacy and modern versions collapsed")
	}
	tools := legacy.CompatibleTools([]contracts.ToolContract{{Name: "object", OutputSchema: []byte(`{"type":"object"}`)}, {Name: "scalar", OutputSchema: []byte(`{"type":"string"}`)}})
	if len(tools) != 1 || tools[0].Name != "object" {
		t.Fatalf("legacy compatibility filter=%+v", tools)
	}
}
