package integration_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/pb33f/libopenapi"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestReferenceAssetsDescribeAValidGateway(t *testing.T) {
	root := repositoryRoot(t)
	schemaPath := filepath.Join(root, "schemas", "gateway-config.v1.schema.json")
	examplePath := filepath.Join(root, "examples", "gateway.yaml")

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaPath)
	if err != nil {
		t.Fatalf("compile gateway schema: %v", err)
	}

	yamlBytes, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read gateway example: %v", err)
	}
	jsonBytes, err := yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		t.Fatalf("convert gateway example to JSON: %v", err)
	}
	var instance any
	if err := json.Unmarshal(jsonBytes, &instance); err != nil {
		t.Fatalf("decode gateway example JSON: %v", err)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatalf("gateway example does not satisfy schema: %v", err)
	}
}

func TestReferenceOpenAPIContainsGetUser(t *testing.T) {
	root := repositoryRoot(t)
	documentBytes, err := os.ReadFile(filepath.Join(root, "testdata", "openapi", "users.openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI fixture: %v", err)
	}
	document, err := libopenapi.NewDocument(documentBytes)
	if err != nil {
		t.Fatalf("parse OpenAPI fixture: %v", err)
	}
	model, err := document.BuildV3Model()
	if err != nil {
		t.Fatalf("build OpenAPI fixture: %v", err)
	}
	path, ok := model.Model.Paths.PathItems.Get("/users/{user_id}")
	if !ok || path.Get == nil {
		t.Fatal("GET /users/{user_id} missing")
	}
	if got, want := path.Get.OperationId, "getUser"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
}

func TestModernRequestFixtureCarriesBodyAndMirrorMetadata(t *testing.T) {
	root := repositoryRoot(t)
	fixture, err := os.ReadFile(filepath.Join(root, "testdata", "protocol", "tools-call.modern.http"))
	if err != nil {
		t.Fatalf("read modern request fixture: %v", err)
	}
	for _, required := range [][]byte{
		[]byte("MCP-Protocol-Version: 2026-07-28"),
		[]byte("MCP-Name: users.get_user"),
		[]byte(`"io.modelcontextprotocol/protocolVersion":"2026-07-28"`),
		[]byte(`"name":"users.get_user"`),
	} {
		if !bytes.Contains(fixture, required) {
			t.Fatalf("modern request fixture missing %q", required)
		}
	}

	result, err := os.ReadFile(filepath.Join(root, "testdata", "protocol", "tools-result.modern.json"))
	if err != nil {
		t.Fatalf("read modern result fixture: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("modern result is not JSON: %v", err)
	}
}

func TestMCPSDKImportsRemainInsideBoundary(t *testing.T) {
	root := repositoryRoot(t)
	internal := filepath.Join(root, "internal")
	allowed := []string{
		filepath.Join(internal, "protocol") + string(filepath.Separator),
		filepath.Join(internal, "adapters", "mcp") + string(filepath.Separator),
	}

	err := filepath.WalkDir(internal, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		for _, prefix := range allowed {
			if strings.HasPrefix(path, prefix) {
				return nil
			}
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Decls {
			declaration, ok := spec.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, item := range declaration.Specs {
				importSpec, ok := item.(*ast.ImportSpec)
				if !ok {
					continue
				}
				importPath, err := strconv.Unquote(importSpec.Path.Value)
				if err != nil {
					return err
				}
				if strings.HasPrefix(importPath, "github.com/modelcontextprotocol/go-sdk/") {
					t.Errorf("MCP SDK import outside protocol boundary: %s imports %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal packages: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
