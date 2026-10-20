package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func FuzzStrictConfiguration(f *testing.F) {
	f.Add([]byte("api_version: mcpgate/v1\n"))
	f.Add([]byte("a: 1\na: 2\n"))
	root := f.TempDir()
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1<<20 {
			return
		}
		file, err := os.CreateTemp(root, "fuzz-*.yaml")
		if err != nil {
			t.Fatal(err)
		}
		name := file.Name()
		defer os.Remove(name)
		if _, err = file.Write(input); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		_, diagnostics := Load(context.Background(), name, SourceLimits{Roots: []string{filepath.Dir(name)}, MaxBytes: 1 << 20, MaxDepth: 64, MaxAliases: 64, MaxNodes: 10000})
		for _, diagnostic := range diagnostics {
			if diagnostic.Line < 1 || diagnostic.Column < 1 || diagnostic.Code == "" {
				t.Fatalf("invalid diagnostic: %+v", diagnostic)
			}
		}
	})
}
