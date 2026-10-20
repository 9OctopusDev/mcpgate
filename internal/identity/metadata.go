package identity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

func MetadataHandler(metadata ProtectedResourceMetadata) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(metadata)
	})
}

func BearerChallenge(resource, code, description string) string {
	quote := func(value string) string {
		value = strings.ReplaceAll(value, "\\", "")
		value = strings.ReplaceAll(value, "\"", "")
		value = strings.ReplaceAll(value, "\r", " ")
		value = strings.ReplaceAll(value, "\n", " ")
		return fmt.Sprintf("%q", value)
	}
	return "Bearer resource=" + quote(resource) + ", error=" + quote(code) + ", error_description=" + quote(description)
}
