package credentials

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/config"
)

func TestCredentialResolveSupportsOnlyEnvAndProtectedFiles(t *testing.T) {
	t.Setenv("MC_GATE_TEST_TOKEN", "env-secret-value")
	binding, err := Resolve(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("resolve env credential: %v", err)
	}
	t.Cleanup(binding.Close)
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err := binding.ApplyHTTP(request); err != nil {
		t.Fatalf("apply env credential: %v", err)
	}
	if got, want := request.Header.Get("Authorization"), "Bearer env-secret-value"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}

	dir := t.TempDir()
	protected := filepath.Join(dir, "protected")
	if err := os.WriteFile(protected, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileBinding, err := Resolve(context.Background(), config.SecretRef{Source: "file", Path: protected})
	if err != nil {
		t.Fatalf("resolve protected file: %v", err)
	}
	fileBinding.Close()

	open := filepath.Join(dir, "open")
	if err := os.WriteFile(open, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), config.SecretRef{Source: "file", Path: open}); err == nil {
		t.Fatal("world-readable secret file accepted")
	}
	if _, err := Resolve(context.Background(), config.SecretRef{Source: "command", Name: "printf"}); err == nil {
		t.Fatal("unsupported command secret provider accepted")
	}
}

func TestCursorCredentialRequiresAtLeast32DecodedBytes(t *testing.T) {
	t.Setenv("MC_GATE_CURSOR", base64.StdEncoding.EncodeToString(make([]byte, 31)))
	if _, err := ResolveCursorKey(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_CURSOR"}); err == nil {
		t.Fatal("31-byte cursor key accepted")
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("MC_GATE_CURSOR", base64.StdEncoding.EncodeToString(key))
	binding, err := ResolveCursorKey(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_CURSOR"})
	if err != nil {
		t.Fatalf("resolve cursor key: %v", err)
	}
	t.Cleanup(binding.Close)
	signature, err := binding.Sign([]byte("catalog-page"))
	if err != nil {
		t.Fatalf("sign cursor: %v", err)
	}
	if err := binding.Verify([]byte("catalog-page"), signature); err != nil {
		t.Fatalf("verify cursor: %v", err)
	}
}

func TestCredentialFormattingAndJSONNeverExposeSecret(t *testing.T) {
	const secret = "needle-secret-value"
	t.Setenv("MC_GATE_REDACT", secret)
	binding, err := Resolve(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_REDACT"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(binding.Close)

	encoded, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{fmt.Sprintf("%v", binding), fmt.Sprintf("%+v", binding), fmt.Sprintf("%#v", binding), string(encoded), RevisionMaterial(binding)}
	for _, output := range outputs {
		if strings.Contains(output, secret) {
			t.Fatalf("secret leaked in %q", output)
		}
	}
}

func TestCredentialRotationCreatesNewEpochWithoutMutatingActiveBinding(t *testing.T) {
	t.Setenv("MC_GATE_ROTATE", "first-secret")
	first, err := Resolve(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_ROTATE"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	t.Setenv("MC_GATE_ROTATE", "second-secret")
	second, err := Resolve(context.Background(), config.SecretRef{Source: "env", Name: "MC_GATE_ROTATE"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	if second.Identity().Epoch <= first.Identity().Epoch {
		t.Fatalf("epochs did not advance: first=%d second=%d", first.Identity().Epoch, second.Identity().Epoch)
	}
	firstRequest, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	secondRequest, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err := first.ApplyHTTP(firstRequest); err != nil {
		t.Fatal(err)
	}
	if err := second.ApplyHTTP(secondRequest); err != nil {
		t.Fatal(err)
	}
	if firstRequest.Header.Get("Authorization") != "Bearer first-secret" || secondRequest.Header.Get("Authorization") != "Bearer second-secret" {
		t.Fatal("rotation mutated an active binding")
	}
}
