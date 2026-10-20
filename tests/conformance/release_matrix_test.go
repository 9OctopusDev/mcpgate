package conformance_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseManifestContainsEveryAcceptanceIDExactlyOnce(t *testing.T) {
	content, err := os.ReadFile("../release-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ ID, Package, Test, Command string }
	if json.Unmarshal(content, &cases) != nil {
		t.Fatal("invalid release manifest")
	}
	expected := make(map[string]bool)
	for prefix, count := range map[string]int{"CFG": 10, "REST": 12, "MCP": 14, "AUTH": 11, "REL": 14} {
		for number := 1; number <= count; number++ {
			expected[fmt.Sprintf("%s-%02d", prefix, number)] = true
		}
	}
	seen := make(map[string]bool)
	for _, entry := range cases {
		if !expected[entry.ID] {
			t.Errorf("unexpected acceptance id %q", entry.ID)
		}
		if seen[entry.ID] {
			t.Errorf("duplicate acceptance id %q", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Package == "" || entry.Test == "" || entry.Command == "" {
			t.Errorf("incomplete evidence for %s", entry.ID)
		}
		if !testExists(t, entry.Package, entry.Test) {
			t.Errorf("%s points to missing %s in %s", entry.ID, entry.Test, entry.Package)
		}
	}
	for id := range expected {
		if !seen[id] {
			t.Errorf("missing acceptance id %s", id)
		}
	}
}

func testExists(t *testing.T, packagePath, testName string) bool {
	t.Helper()
	directory := filepath.Join("..", "..", strings.TrimPrefix(packagePath, "./"))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Error(err)
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, 0)
		if err != nil {
			t.Error(err)
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == testName {
				return true
			}
		}
	}
	return false
}
