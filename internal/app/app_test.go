package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunVersionJSONReportsBuildAndCompatibility(t *testing.T) {
	oldVersion, oldCommit, oldGo := Version, Commit, BuildGoVersion
	Version, Commit, BuildGoVersion = "1.0.0-test", "abc123", "go1.27.3"
	t.Cleanup(func() { Version, Commit, BuildGoVersion = oldVersion, oldCommit, oldGo })

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(version) code = %d, stderr = %q", code, stderr.String())
	}

	var got struct {
		Version       string            `json:"version"`
		Commit        string            `json:"commit"`
		GoVersion     string            `json:"go_version"`
		ConfigVersion string            `json:"config_version"`
		MCPVersions   []string          `json:"mcp_versions"`
		Dependencies  map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode version JSON: %v", err)
	}
	if got.Version != "1.0.0-test" || got.Commit != "abc123" || got.GoVersion != "go1.27.3" {
		t.Fatalf("unexpected build identity: %+v", got)
	}
	if got.ConfigVersion != "mcpgate/v1" {
		t.Fatalf("config_version = %q", got.ConfigVersion)
	}
	if strings.Join(got.MCPVersions, ",") != "2026-07-28,2025-11-25" {
		t.Fatalf("mcp_versions = %v", got.MCPVersions)
	}
	if got.Dependencies["github.com/modelcontextprotocol/go-sdk"] != "v1.8.0" {
		t.Fatalf("MCP SDK version missing: %v", got.Dependencies)
	}
}

func TestRunValidateAndInspectExample(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := "../../examples/gateway.yaml"
	if code := Run(context.Background(), []string{"validate", "--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(validate) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"valid":true`) || !strings.Contains(stdout.String(), `"online":false`) {
		t.Fatalf("validate output=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"inspect", "--config", configPath, "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(inspect) code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"exports"`) || strings.Contains(stdout.String(), "downstream-example") {
		t.Fatalf("unsafe inspect output=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"serve"}, &stdout, &stderr); code != 2 {
		t.Fatalf("serve without config code=%d", code)
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Fatalf("serve stderr=%q", stderr.String())
	}
}
