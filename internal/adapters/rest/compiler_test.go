package rest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "openapi", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func compileFixture(t *testing.T, name, operation string, bindings map[string]bool) *RESTPlan {
	t.Helper()
	plan, err := Compile(fixture(t, name), CompileOptions{
		OperationID: operation, EndpointBase: "https://operator.example/api/v1", SecurityBindings: bindings,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestCompileCollisionSafeGroupedSchema(t *testing.T) {
	plan := compileFixture(t, "collisions.yaml", "getThing", map[string]bool{"bearerAuth": true})
	var schema map[string]any
	if err := json.Unmarshal(plan.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	pathGroup := properties["path"].(map[string]any)["properties"].(map[string]any)
	queryGroup := properties["query"].(map[string]any)["properties"].(map[string]any)
	if pathGroup["id"] == nil || queryGroup["id"] == nil || properties["body"] == nil {
		t.Fatalf("colliding parameters were not grouped safely: %s", plan.InputSchema)
	}
	if strings.Contains(string(plan.InputSchema), "Authorization") || strings.Contains(string(plan.InputSchema), "operator.example") {
		t.Fatalf("authority leaked into model input: %s", plan.InputSchema)
	}
	path, err := plan.BuildPath(map[string]any{"id": "u_123"})
	if err != nil || path != "/things/u_123" {
		t.Fatalf("path = %q, %v", path, err)
	}
	for _, unsafe := range []string{"../admin", "a/b", `a\b`, "%2fadmin", "%2e%2e"} {
		if _, err := plan.BuildPath(map[string]any{"id": unsafe}); !errors.Is(err, ErrInvalidParameter) {
			t.Fatalf("unsafe path value %q accepted: %v", unsafe, err)
		}
	}
}

func TestQueryParameterSerializationPreservesValues(t *testing.T) {
	plan := compileFixture(t, "serialization.yaml", "search", nil)
	query, err := plan.BuildQuery(map[string]any{
		"active": false, "limit": json.Number("0"), "empty": "", "nil": nil,
		"tags": []any{"a", "b"}, "ids": []any{json.Number("1"), json.Number("2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	const expected = "active=false&empty=&ids=1%2C2&limit=0&nil=null&tags=a&tags=b"
	if query != expected {
		t.Fatalf("query = %q, want %q", query, expected)
	}
	query, err = plan.BuildQuery(map[string]any{})
	if err != nil || query != "" {
		t.Fatalf("omitted values were emitted: %q %v", query, err)
	}
	query, err = plan.BuildQuery(map[string]any{"limit": json.Number("900719925474099312345")})
	if err != nil || query != "limit=900719925474099312345" {
		t.Fatalf("large integer lost precision: %q %v", query, err)
	}
}

func TestCompileRejectsUnsupportedParametersAndMappings(t *testing.T) {
	tests := map[string]string{
		"query object":    `parameters: [{name: filter, in: query, schema: {type: object}}]`,
		"deep object":     `parameters: [{name: filter, in: query, style: deepObject, schema: {type: object}}]`,
		"space delimited": `parameters: [{name: tags, in: query, style: spaceDelimited, schema: {type: array, items: {type: string}}}]`,
		"header":          `parameters: [{name: X-Mode, in: header, schema: {type: string}}]`,
		"cookie":          `parameters: [{name: session, in: cookie, schema: {type: string}}]`,
	}
	for name, parameters := range tests {
		t.Run(name, func(t *testing.T) {
			document := []byte("openapi: 3.1.0\ninfo: {title: x, version: v}\npaths:\n  /x:\n    get:\n      operationId: op\n      " + parameters + "\n      responses: {'200': {description: ok}}\n")
			if _, err := Compile(document, CompileOptions{OperationID: "op", EndpointBase: "https://operator.example"}); !errors.Is(err, ErrUnsupportedOpenAPI) {
				t.Fatalf("unsupported mapping accepted: %v", err)
			}
		})
	}

	multipart := []byte("openapi: 3.1.0\ninfo: {title: x, version: v}\npaths:\n  /x:\n    post:\n      operationId: op\n      requestBody:\n        content:\n          multipart/form-data:\n            schema: {type: object}\n      responses: {'200': {description: ok}}\n")
	if _, err := Compile(multipart, CompileOptions{OperationID: "op", EndpointBase: "https://operator.example"}); !errors.Is(err, ErrUnsupportedOpenAPI) {
		t.Fatalf("multipart accepted: %v", err)
	}
}

func TestCompileOperationIDsAndSecurityAlternatives(t *testing.T) {
	duplicate := []byte("openapi: 3.1.0\ninfo: {title: x, version: v}\npaths:\n  /a:\n    get: {operationId: same, responses: {'200': {description: ok}}}\n  /b:\n    get: {operationId: same, responses: {'200': {description: ok}}}\n")
	if _, err := Compile(duplicate, CompileOptions{OperationID: "same", EndpointBase: "https://operator.example"}); !errors.Is(err, ErrDuplicateOperationID) {
		t.Fatalf("duplicate operation IDs accepted: %v", err)
	}
	if _, err := Compile(fixture(t, "security.yaml"), CompileOptions{OperationID: "absent", EndpointBase: "https://operator.example"}); !errors.Is(err, ErrOperationNotFound) {
		t.Fatalf("absent operation accepted: %v", err)
	}
	if _, err := Compile(fixture(t, "security.yaml"), CompileOptions{OperationID: "both", EndpointBase: "https://operator.example", SecurityBindings: map[string]bool{"oauth": true}}); !errors.Is(err, ErrSecurityUnsatisfied) {
		t.Fatalf("conjunctive requirement accepted without every scheme: %v", err)
	}
	plan := compileFixture(t, "security.yaml", "either", map[string]bool{"oauth": true})
	if len(plan.SecuritySchemes) != 1 || plan.SecuritySchemes[0] != "oauth" {
		t.Fatalf("security alternative not selected: %#v", plan.SecuritySchemes)
	}
}

func TestSchemaNormalizationAndComposition(t *testing.T) {
	plan := compileFixture(t, "nullable.yaml", "create", nil)
	text := string(plan.InputSchema)
	if !strings.Contains(text, `"null"`) || strings.Contains(text, `"nullable"`) || !strings.Contains(text, `"exclusiveMinimum":0`) {
		t.Fatalf("OpenAPI 3.0 schema was not normalized: %s", text)
	}
	if strings.Contains(text, "output_only") {
		t.Fatalf("unambiguous read-only property remained in request schema: %s", text)
	}
	composition := compileFixture(t, "composition.yaml", "compose", nil)
	if !strings.Contains(string(composition.InputSchema), `"oneOf"`) || !strings.Contains(string(composition.InputSchema), `"allOf"`) {
		t.Fatalf("composition was not preserved: %s", composition.InputSchema)
	}
	recursive := compileFixture(t, "recursive.yaml", "recursive", nil)
	if !strings.Contains(string(recursive.InputSchema), `"$defs"`) || !strings.Contains(string(recursive.InputSchema), `"#/$defs/Node"`) {
		t.Fatalf("local recursive schema was not retained safely: %s", recursive.InputSchema)
	}
}

func TestCompileRejectsExternalReferencesAndIgnoresDocumentServers(t *testing.T) {
	if _, err := Compile(fixture(t, "external-ref.yaml"), CompileOptions{OperationID: "external", EndpointBase: "https://operator.example/root"}); !errors.Is(err, ErrExternalReference) {
		t.Fatalf("external reference accepted: %v", err)
	}
	plan := compileFixture(t, "collisions.yaml", "getThing", map[string]bool{"bearerAuth": true})
	if plan.EndpointBase != "https://operator.example/api/v1" || strings.Contains(plan.EndpointBase, "attacker") {
		t.Fatalf("document server influenced endpoint: %q", plan.EndpointBase)
	}
}
