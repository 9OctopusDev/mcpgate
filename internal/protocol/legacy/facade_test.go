package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
	"github.com/9OctopusDev/mcpgate/internal/executor"
)

type executorFunc func(context.Context, executor.Request, contracts.EventSink) (contracts.Outcome, error)

func (fn executorFunc) Execute(ctx context.Context, request executor.Request, sink contracts.EventSink) (contracts.Outcome, error) {
	return fn(ctx, request, sink)
}

func TestLegacyRequiresInitializeThenInitializedAndAuthenticatesSession(t *testing.T) {
	principal := contracts.Principal{PartitionDigest: "p", Subject: "s"}
	dependencies := Dependencies{ServerID: "server", Sessions: NewSessions(10, 15*time.Minute, time.Now), Principal: func(*http.Request) (contracts.Principal, bool) { return principal, true }}
	handler := Handler(dependencies)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"old","version":"1"},"capabilities":{"tools":{}}}}`
	request := httptest.NewRequest(http.MethodPost, "/legacy", strings.NewReader(initialize))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	sessionID := response.Header().Get("Mcp-Session-Id")
	if response.Code != 200 || sessionID == "" {
		t.Fatalf("status=%d session=%q body=%s", response.Code, sessionID, response.Body.String())
	}

	call := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	request = httptest.NewRequest(http.MethodPost, "/legacy", strings.NewReader(call))
	request.Header.Set("Mcp-Session-Id", sessionID)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "not initialized") {
		t.Fatalf("pre-initialized call accepted: %s", response.Body.String())
	}

	notification := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	request = httptest.NewRequest(http.MethodPost, "/legacy", strings.NewReader(notification))
	request.Header.Set("Mcp-Session-Id", sessionID)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("initialized status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLegacyCallTranslatesCompleteResult(t *testing.T) {
	principal := contracts.Principal{PartitionDigest: "p"}
	sessions := NewSessions(10, time.Hour, time.Now)
	session, _ := sessions.Create("server", principal, nil)
	sessions.MarkInitialized(session.ID)
	route := contracts.Route{Key: contracts.RouteKey{ServerID: "server", PublicName: "echo"}, NativeName: "native.echo"}
	handler := Handler(Dependencies{ServerID: "server", Sessions: sessions, Principal: func(*http.Request) (contracts.Principal, bool) { return principal, true }, Resolve: func(name string) (contracts.Route, bool) { return route, name == "echo" }, Execute: executorFunc(func(_ context.Context, request executor.Request, _ contracts.EventSink) (contracts.Outcome, error) {
		return contracts.Outcome{Complete: contracts.NewToolResult(json.RawMessage(`{"ok":true}`), []contracts.ContentBlock{{Type: "text", Text: "ok"}}, false)}, nil
	})})
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{}}}`
	request := httptest.NewRequest(http.MethodPost, "/legacy", strings.NewReader(body))
	request.Header.Set("Mcp-Session-Id", session.ID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"structuredContent":{"ok":true}`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/legacy", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", response.Code)
	}
}
