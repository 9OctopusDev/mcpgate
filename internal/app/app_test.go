package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRunServeStartsConfiguredListenersAndStopsWithContext(t *testing.T) {
	dataAddress := unusedLoopbackAddress(t)
	adminAddress := unusedLoopbackAddress(t)
	directory := t.TempDir()
	configDirectory := filepath.Join(directory, "config")
	openAPIDirectory := filepath.Join(directory, "openapi")
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(openAPIDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	openAPI, err := os.ReadFile("../../testdata/openapi/users.openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(openAPIDirectory, "users.openapi.yaml"), openAPI, 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, err := os.ReadFile("../../testdata/config/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.ReplaceAll(string(configuration), "127.0.0.1:8080", dataAddress)
	configured = strings.ReplaceAll(configured, "localhost:8080", dataAddress)
	configured = strings.ReplaceAll(configured, "127.0.0.1:9090", adminAddress)
	path := filepath.Join(configDirectory, "gateway.yaml")
	if err := os.WriteFile(path, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_TOKEN", "upstream-test-token")
	t.Setenv("TEST_CURSOR_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32)))
	if _, report := validateDocument(context.Background(), path, true); !report.Valid {
		t.Fatalf("test configuration is invalid: %+v", report.Diagnostics)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	go func() { result <- Run(ctx, []string{"serve", "--config", path}, &stdout, &stderr) }()

	readyURL := "http://" + adminAddress + "/readyz"
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, requestErr := http.Get(readyURL)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case code := <-result:
			t.Fatalf("serve exited before readiness with code %d: %s", code, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin readiness was not served: %s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("serve shutdown code = %d: %s", code, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}
}

func unusedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
