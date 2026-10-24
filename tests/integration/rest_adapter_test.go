package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/adapters/rest"
	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/credentials"
	"github.com/9OctopusDev/mcpgate/internal/testkit"
)

func TestRESTAdapterEndToEndPlanCredentialAndResult(t *testing.T) {
	document := []byte(`openapi: 3.1.0
info: {title: Ping, version: v1}
paths:
  /ping/{id}:
    get:
      operationId: ping
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: ok
          content: {application/json: {schema: {type: object, required: [id], properties: {id: {type: string}}}}}
`)
	plan, err := rest.Compile(document, rest.CompileOptions{OperationID: "ping", EndpointBase: "https://api.example/base"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCPGATE_REST_TEST_TOKEN", "service-token")
	credential, err := credentials.Resolve(context.Background(), config.SecretRef{Source: "env", Name: "MCPGATE_REST_TEST_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	defer credential.Close()
	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer service-token" {
			t.Errorf("credential not applied")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"p_1"}`))
	})}
	adapter, err := rest.NewAdapter(map[string]*rest.RESTPlan{"ping": plan}, &http.Client{Transport: recording}, credential, rest.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := adapter.CallTool(context.Background(), contracts.Call{
		ExecutionID: "integration-1", Route: contracts.Route{NativeName: "ping"}, Arguments: json.RawMessage(`{"path":{"id":"p_1"}}`),
	}, nil)
	if err != nil || outcome.Complete == nil || outcome.Complete.IsError {
		t.Fatalf("unexpected REST outcome: %+v %v", outcome, err)
	}
	requests := recording.Requests()
	if len(requests) != 1 || requests[0].URL != "https://api.example/base/ping/p_1" {
		t.Fatalf("unexpected request: %+v", requests)
	}
}
