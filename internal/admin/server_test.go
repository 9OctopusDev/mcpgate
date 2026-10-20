package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"
)

type provider struct{ status gatewayruntime.Status }

func (p provider) Status() gatewayruntime.Status { return p.status }
func (provider) Upstreams() any                  { return []any{map[string]any{"id": "users", "state": "READY"}} }
func (provider) Routes() any                     { return []any{map[string]any{"name": "users.get"}} }

func TestHealthAndStatusHandlersReflectReadiness(t *testing.T) {
	handler, err := New("127.0.0.1:9090", provider{status: gatewayruntime.Status{Ready: false, Reason: "discovery pending"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		status int
	}{{"/livez", 200}, {"/readyz", 503}, {"/v1/status", 200}, {"/v1/upstreams", 200}, {"/v1/routes", 200}} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s status=%d", test.path, response.Code)
		}
		if test.path == "/v1/status" {
			var value gatewayruntime.Status
			if json.Unmarshal(response.Body.Bytes(), &value) != nil || value.Reason != "discovery pending" {
				t.Fatalf("body=%s", response.Body.String())
			}
		}
	}
}

func TestAdminRejectsNonLoopbackBind(t *testing.T) {
	if _, err := New("0.0.0.0:9090", provider{}, nil); err == nil {
		t.Fatal("public admin bind accepted")
	}
}
