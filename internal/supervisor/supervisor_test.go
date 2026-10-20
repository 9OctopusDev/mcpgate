package supervisor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestStdioLaunchBoundaryValidation(t *testing.T) {
	valid := Config{Executable: "/opt/mcpgate/bin/upstream", Args: []string{"--stdio"}, CWD: "/var/empty", Env: map[string]string{"LANG": "C"}, MaxProcesses: 2, MaxInflight: 1, MaxFrameBytes: 1024, StartupAttempts: 2, Grace: time.Second}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"relative executable": func(c *Config) { c.Executable = "upstream" },
		"shell":               func(c *Config) { c.Executable = "/bin/sh" },
		"relative cwd":        func(c *Config) { c.CWD = "." },
		"unsafe env":          func(c *Config) { c.Env["LD_PRELOAD"] = "/tmp/x.so" },
		"substitution":        func(c *Config) { c.Args = []string{"$(id)"} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Args = append([]string(nil), valid.Args...)
			candidate.Env = map[string]string{"LANG": "C"}
			mutate(&candidate)
			if candidate.Validate() == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestFrameReaderRejectsOversizeAndMalformedJSON(t *testing.T) {
	reader := NewFrameReader(strings.NewReader("{\"jsonrpc\":\"2.0\"}\n"), 64)
	frame, err := reader.Read()
	if err != nil || string(frame) != `{"jsonrpc":"2.0"}` {
		t.Fatalf("frame=%s err=%v", frame, err)
	}
	if _, err := NewFrameReader(strings.NewReader(strings.Repeat("x", 65)+"\n"), 64).Read(); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := NewFrameReader(bytes.NewBufferString("not-json\n"), 64).Read(); err == nil {
		t.Fatal("malformed frame accepted")
	}
}

func TestPartitionKeySeparatesPrincipalCredentialAndSession(t *testing.T) {
	base := PartitionKey{Generation: "g", Tenant: "t", Principal: "p", CredentialEpoch: 1, Session: "s"}
	variants := []PartitionKey{{Generation: "g2", Tenant: "t", Principal: "p", CredentialEpoch: 1, Session: "s"}, {Generation: "g", Tenant: "t", Principal: "other", CredentialEpoch: 1, Session: "s"}, {Generation: "g", Tenant: "t", Principal: "p", CredentialEpoch: 2, Session: "s"}, {Generation: "g", Tenant: "t", Principal: "p", CredentialEpoch: 1, Session: "other"}}
	for _, variant := range variants {
		if base.String() == variant.String() {
			t.Fatalf("partition collision: %+v", variant)
		}
	}
}

func TestBackoffIsBounded(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		delay := Backoff(attempt, time.Second, time.Minute)
		if delay < 0 || delay > time.Minute {
			t.Fatalf("attempt=%d delay=%s", attempt, delay)
		}
	}
	if err := context.Canceled; err == nil {
		t.Fatal("unreachable")
	}
}
