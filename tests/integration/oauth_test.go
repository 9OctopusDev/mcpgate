package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/identity"
)

func TestOAuthMetadataIsScopedPerVirtualServer(t *testing.T) {
	team := identity.ProtectedResourceMetadata{
		Resource:             "https://gateway.example/mcp/team",
		AuthorizationServers: []string{"https://issuer.example"},
		ScopesSupported:      []string{"team:tools"},
	}
	admin := identity.ProtectedResourceMetadata{
		Resource:             "https://gateway.example/mcp/admin",
		AuthorizationServers: []string{"https://issuer.example"},
		ScopesSupported:      []string{"admin:tools"},
	}

	for name, metadata := range map[string]identity.ProtectedResourceMetadata{"team": team, "admin": admin} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			identity.MetadataHandler(metadata).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("metadata status = %d", recorder.Code)
			}
			var received identity.ProtectedResourceMetadata
			if err := json.Unmarshal(recorder.Body.Bytes(), &received); err != nil {
				t.Fatal(err)
			}
			if received.Resource != metadata.Resource || received.ScopesSupported[0] != metadata.ScopesSupported[0] {
				t.Fatalf("virtual server metadata crossed boundaries: %+v", received)
			}
		})
	}
}
