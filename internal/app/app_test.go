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

func TestRunKnownUnimplementedCommandReturnsUsageCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"serve", "--config", "gateway.yaml"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Run(serve) code = %d, want 2", code)
	}
	if got := stderr.String(); got != "serve is not implemented\n" {
		t.Fatalf("stderr = %q", got)
	}
}
