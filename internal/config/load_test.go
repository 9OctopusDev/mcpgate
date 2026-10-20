package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStrictLoadAcceptsValidDocumentAndAppliesDefaults(t *testing.T) {
	path := fixturePath(t, "valid.yaml")
	doc, diagnostics := Load(context.Background(), path, SourceLimits{Roots: []string{filepath.Dir(path)}})
	if len(diagnostics) != 0 {
		t.Fatalf("valid config diagnostics: %+v", diagnostics)
	}
	if doc == nil {
		t.Fatal("valid config returned nil document")
	}
	if got, want := doc.Gateway.Limits.MaxInflight, 256; got != want {
		t.Fatalf("max_inflight = %d, want %d", got, want)
	}
	if got, want := doc.Gateway.Limits.QueueTimeout.Duration, 250*time.Millisecond; got != want {
		t.Fatalf("queue_timeout = %s, want %s", got, want)
	}
}

func TestStrictLoadAppliesDocumentedDefaultsWhenBudgetsAreOmitted(t *testing.T) {
	source, err := os.ReadFile(fixturePath(t, "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	limitsBlock := `  limits:
    max_inflight: 256
    max_queued: 64
    queue_timeout: 250ms
    call_timeout: 30s
    request_bytes: 1048576
    response_bytes: 4194304
    schema_bytes: 2097152
    buffer_budget_bytes: 268435456
    per_principal_rps: 20
    per_principal_burst: 40
`
	lifecycleBlock := `  lifecycle:
    startup_timeout: 30s
    drain_timeout: 45s
    legacy_session_idle: 15m
    max_legacy_sessions: 1000
`
	content := strings.Replace(string(source), limitsBlock, "  limits: {}\n", 1)
	content = strings.Replace(content, lifecycleBlock, "  lifecycle: {}\n", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "defaults.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, diagnostics := Load(context.Background(), path, SourceLimits{Roots: []string{dir}})
	if len(diagnostics) != 0 {
		t.Fatalf("defaults config diagnostics: %+v", diagnostics)
	}
	if got, want := doc.Gateway.Limits.BufferBudgetBytes, int64(256<<20); got != want {
		t.Fatalf("buffer budget = %d, want %d", got, want)
	}
	if got, want := doc.Gateway.Lifecycle.LegacySessionIdle.Duration, 15*time.Minute; got != want {
		t.Fatalf("legacy session idle = %s, want %s", got, want)
	}
}

func TestStrictLoadRejectsAmbiguousOrUnsupportedYAML(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		limits SourceLimits
		code   string
	}{
		{name: "duplicate key", file: "duplicate-key.yaml", code: "YAML_DUPLICATE_KEY"},
		{name: "unknown field", file: "unknown-field.yaml", code: "CONFIG_SCHEMA_INVALID"},
		{name: "custom tag", file: "custom-tag.yaml", code: "YAML_CUSTOM_TAG"},
		{name: "alias budget", file: "alias-bomb.yaml", limits: SourceLimits{MaxAliases: 2}, code: "YAML_ALIAS_LIMIT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fixturePath(t, tt.file)
			limits := tt.limits
			limits.Roots = []string{filepath.Dir(path)}
			doc, diagnostics := Load(context.Background(), path, limits)
			if doc != nil {
				t.Fatal("invalid config returned a document")
			}
			assertDiagnostic(t, diagnostics, tt.code, path)
		})
	}
}

func TestStrictLoadRejectsMultipleDocumentsAndDepth(t *testing.T) {
	dir := t.TempDir()
	multiple := filepath.Join(dir, "multiple.yaml")
	if err := os.WriteFile(multiple, []byte("api_version: mcpgate/v1\n---\napi_version: mcpgate/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if doc, diagnostics := Load(context.Background(), multiple, SourceLimits{Roots: []string{dir}}); doc != nil {
		t.Fatal("multiple documents accepted")
	} else {
		assertDiagnostic(t, diagnostics, "YAML_DOCUMENT_COUNT", multiple)
	}

	deep := filepath.Join(dir, "deep.yaml")
	if err := os.WriteFile(deep, []byte("a:\n  b:\n    c:\n      d: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if doc, diagnostics := Load(context.Background(), deep, SourceLimits{Roots: []string{dir}, MaxDepth: 3}); doc != nil {
		t.Fatal("over-depth document accepted")
	} else {
		assertDiagnostic(t, diagnostics, "YAML_DEPTH_LIMIT", deep)
	}
}

func TestBoundsRejectsOversizedConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.yaml")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 33)), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, diagnostics := Load(context.Background(), path, SourceLimits{Roots: []string{dir}, MaxBytes: 32})
	if doc != nil {
		t.Fatal("oversized document accepted")
	}
	assertDiagnostic(t, diagnostics, "SOURCE_TOO_LARGE", path)
}

func TestDiagnosticFieldsArePopulated(t *testing.T) {
	path := fixturePath(t, "duplicate-key.yaml")
	_, diagnostics := Load(context.Background(), path, SourceLimits{Roots: []string{filepath.Dir(path)}})
	if len(diagnostics) == 0 {
		t.Fatal("missing diagnostic")
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code == "" || diagnostic.File == "" || diagnostic.Line < 1 || diagnostic.Column < 1 || diagnostic.ConfigPath == "" || diagnostic.Message == "" {
		t.Fatalf("incomplete diagnostic: %+v", diagnostic)
	}
}

func assertDiagnostic(t *testing.T, diagnostics []Diagnostic, code, path string) {
	t.Helper()
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one", diagnostics)
	}
	if got := diagnostics[0].Code; got != code {
		t.Fatalf("diagnostic code = %q, want %q (%+v)", got, code, diagnostics[0])
	}
	if got := diagnostics[0].File; got != path {
		t.Fatalf("diagnostic file = %q, want %q", got, path)
	}
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve fixture path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "testdata", "config", name))
}
