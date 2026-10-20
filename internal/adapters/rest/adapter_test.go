package rest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mcpgate/mcpgate/internal/contracts"
	"github.com/mcpgate/mcpgate/internal/testkit"
)

type bearerCredential struct{ token string }

func (credential *bearerCredential) Identity() contracts.CredentialIdentity {
	return contracts.CredentialIdentity{Reference: "test", Epoch: 1}
}
func (credential *bearerCredential) ApplyHTTP(request *http.Request) error {
	request.Header.Set("Authorization", "Bearer "+credential.token)
	return nil
}
func (credential *bearerCredential) Sign([]byte) ([]byte, error) {
	return nil, contracts.ErrCredentialOperationUnsupported
}
func (credential *bearerCredential) Verify([]byte, []byte) error {
	return contracts.ErrCredentialOperationUnsupported
}
func (credential *bearerCredential) Close() {}

func usersPlan(t *testing.T, responseSchema string) *RESTPlan {
	t.Helper()
	document := `openapi: 3.1.0
info: {title: Users, version: v1}
paths:
  /users/{user_id}:
    get:
      operationId: getUser
      parameters:
        - {name: user_id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: ` + responseSchema + "\n"
	plan, err := Compile([]byte(document), CompileOptions{OperationID: "getUser", EndpointBase: "https://upstream.example/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func callFixture(arguments string) contracts.Call {
	return contracts.Call{
		ExecutionID: "exec-1", Route: contracts.Route{NativeName: "getUser"}, Arguments: json.RawMessage(arguments),
	}
}

func TestRESTVerticalSliceBuildsFreshRequestAndEnvelope(t *testing.T) {
	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"u_123","display_name":"Example User"}`))
	})}
	adapter, err := NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object, required: [id], properties: {id: {type: string}, display_name: {type: string}}}`)}, &http.Client{Transport: recording}, &bearerCredential{token: "upstream-token"}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_123"}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := outcome.Validate(); err != nil || outcome.Complete == nil || outcome.Complete.IsError {
		t.Fatalf("unexpected outcome: %+v %v", outcome, err)
	}
	requests := recording.Requests()
	if len(requests) != 1 || requests[0].Method != http.MethodGet || requests[0].URL != "https://upstream.example/api/v1/users/u_123" {
		t.Fatalf("unexpected upstream request: %+v", requests)
	}
	if requests[0].Header.Get("Authorization") != "Bearer upstream-token" {
		t.Fatalf("credential missing: %#v", requests[0].Header)
	}
	for _, forbidden := range []string{"Cookie", "Forwarded", "Host"} {
		if requests[0].Header.Get(forbidden) != "" {
			t.Fatalf("%s leaked upstream", forbidden)
		}
	}
	if !strings.Contains(string(outcome.Complete.Structured), `"ok":true`) || !strings.Contains(string(outcome.Complete.Structured), `"status":200`) {
		t.Fatalf("invalid structured envelope: %s", outcome.Complete.Structured)
	}
	if len(outcome.Complete.Content) != 1 || outcome.Complete.Content[0].Text != string(outcome.Complete.Structured) {
		t.Fatalf("compact text does not match structured envelope: %+v", outcome.Complete.Content)
	}
}

func TestRESTNormalizesJSONTextNoContentAndAccepted(t *testing.T) {
	tests := []struct {
		name                    string
		status                  int
		contentType, body, want string
	}{
		{name: "json", status: 200, contentType: "application/json", body: `{"id":"u_1"}`, want: `"status":200`},
		{name: "text", status: 200, contentType: "text/plain", body: `ready`, want: `"data":"ready"`},
		{name: "no content", status: 204, want: `"data":null`},
		{name: "accepted", status: 202, contentType: "application/json", body: `{"job":"j_1"}`, want: `"status":202`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if test.contentType != "" {
					response.Header().Set("Content-Type", test.contentType)
				}
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			})}
			plan := usersPlan(t, `{type: object}`)
			if test.contentType == "text/plain" {
				plan.ResponseSchemas = nil
			}
			adapter, _ := NewAdapter(map[string]*RESTPlan{"getUser": plan}, &http.Client{Transport: recording}, &bearerCredential{}, Limits{})
			outcome, err := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
			if err != nil || outcome.Complete == nil || outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), test.want) {
				t.Fatalf("unexpected normalized result: %+v %v", outcome, err)
			}
		})
	}
}

func TestRESTMapsBusinessAuthProtocolAndUnknownFailures(t *testing.T) {
	for _, status := range []int{404, 409, 422} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(status)
				_, _ = response.Write([]byte("unsafe detail"))
			})}
			adapter, _ := NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object}`)}, &http.Client{Transport: recording}, &bearerCredential{}, Limits{})
			outcome, err := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
			if err != nil || outcome.Complete == nil || !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "UPSTREAM_BUSINESS_ERROR") || strings.Contains(string(outcome.Complete.Structured), "unsafe detail") {
				t.Fatalf("unsafe business outcome: %+v %v", outcome, err)
			}
		})
	}

	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("WWW-Authenticate", `Bearer realm="upstream"`)
		response.WriteHeader(401)
	})}
	adapter, _ := NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object}`)}, &http.Client{Transport: recording}, &bearerCredential{}, Limits{})
	outcome, _ := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
	if !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "UPSTREAM_AUTH_FAILED") || strings.Contains(string(outcome.Complete.Structured), "realm") {
		t.Fatalf("upstream auth leaked: %+v", outcome)
	}

	malformed := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"wrong":true}`))
	})}
	adapter, _ = NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object, required: [id], properties: {id: {type: string}}}`)}, &http.Client{Transport: malformed}, &bearerCredential{}, Limits{})
	outcome, _ = adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
	if !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "UPSTREAM_PROTOCOL_ERROR") {
		t.Fatalf("schema violation accepted: %+v", outcome)
	}

	failingClient := &http.Client{Transport: roundTripError{err: errors.New("connection dropped")}}
	adapter, _ = NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object}`)}, failingClient, &bearerCredential{}, Limits{})
	outcome, _ = adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
	if !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "UPSTREAM_OUTCOME_UNKNOWN") || !strings.Contains(string(outcome.Complete.Structured), "exec-1") {
		t.Fatalf("unknown outcome not explicit: %+v", outcome)
	}
}

type roundTripError struct{ err error }

func (transport roundTripError) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.err
}

func TestRESTRejectsOversizedAndInvalidArguments(t *testing.T) {
	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		_, _ = response.Write([]byte(strings.Repeat("x", 128)))
	})}
	adapter, _ := NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object}`)}, &http.Client{Transport: recording}, &bearerCredential{}, Limits{ResponseBytes: 32, EncodedResponseBytes: 64, OutputBytes: 128})
	outcome, _ := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
	if !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "RESULT_TOO_LARGE") {
		t.Fatalf("oversized result accepted: %+v", outcome)
	}

	outcome, _ = adapter.CallTool(context.Background(), callFixture(`{"path":{}}`), nil)
	if !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "INVALID_ARGUMENTS") || len(recording.Requests()) != 1 {
		t.Fatalf("invalid arguments reached upstream: %+v requests=%d", outcome, len(recording.Requests()))
	}
}

func TestRESTRejectsCompressedBombAfterDecompression(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(strings.Repeat("x", 256)))
	_ = writer.Close()
	recording := &testkit.RecordingHTTP{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		response.Header().Set("Content-Encoding", "gzip")
		_, _ = response.Write(compressed.Bytes())
	})}
	adapter, _ := NewAdapter(map[string]*RESTPlan{"getUser": usersPlan(t, `{type: object}`)}, &http.Client{Transport: recording}, &bearerCredential{}, Limits{EncodedResponseBytes: 128, ResponseBytes: 32})
	outcome, _ := adapter.CallTool(context.Background(), callFixture(`{"path":{"user_id":"u_1"}}`), nil)
	if outcome.Complete == nil || !outcome.Complete.IsError || !strings.Contains(string(outcome.Complete.Structured), "RESULT_TOO_LARGE") {
		t.Fatalf("compressed bomb accepted: %+v", outcome)
	}
}
