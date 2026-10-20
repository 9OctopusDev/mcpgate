package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceResolvesRelativePathAgainstConfigurationDirectory(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "source.yaml")
	if err := os.WriteFile(path, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ref, err := ReadLocal(configDir, "source.yaml", []string{configDir}, 16)
	if err != nil {
		t.Fatalf("ReadLocal: %v", err)
	}
	if got, want := string(content), "safe"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if ref.Resolved != path || ref.Size != 4 {
		t.Fatalf("source ref = %+v", ref)
	}
}

func TestSymlinkEscapeIsRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.yaml")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.yaml")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ReadLocal(root, "escape.yaml", []string{root}, 64); err == nil {
		t.Fatal("symlink escape accepted")
	} else if sourceErr, ok := err.(*SourceError); !ok || sourceErr.Code != "SOURCE_ROOT_ESCAPE" {
		t.Fatalf("error = %T %v, want SOURCE_ROOT_ESCAPE", err, err)
	}
}

func TestSourceBoundsUseLimitPlusOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.yaml")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadLocal(dir, "source.yaml", []string{dir}, 4); err == nil {
		t.Fatal("oversized source accepted")
	} else if sourceErr, ok := err.(*SourceError); !ok || sourceErr.Code != "SOURCE_TOO_LARGE" {
		t.Fatalf("error = %T %v, want SOURCE_TOO_LARGE", err, err)
	}
}
