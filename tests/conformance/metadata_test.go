package conformance_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSDKImportsRemainInsideProtocolBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".tools" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, declaration := range file.Imports {
			importPath, err := strconv.Unquote(declaration.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(importPath, "github.com/modelcontextprotocol/go-sdk") {
				slash := filepath.ToSlash(path)
				if !strings.Contains(slash, "/internal/protocol/") && !strings.Contains(slash, "/internal/adapters/mcp/") {
					t.Errorf("SDK import outside boundary: %s", slash)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
