package faults_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/supervisor"
)

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("MCPGATE_STDIO_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	var writer sync.Mutex
	for scanner.Scan() {
		frame := append([]byte(nil), scanner.Bytes()...)
		go func() {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			if json.Unmarshal(frame, &request) != nil || request.Method != "tools/call" {
				return
			}
			if request.Params.Name == "slow" {
				time.Sleep(200 * time.Millisecond)
			}
			if request.Params.Name == "hang" {
				time.Sleep(time.Hour)
			}
			response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": "ok"}}}})
			writer.Lock()
			fmt.Fprintln(os.Stdout, string(response))
			writer.Unlock()
		}()
	}
	os.Exit(0)
}

func processConfig(t *testing.T, inflight int) supervisor.Config {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor.Config{Executable: executable, Args: []string{"-test.run=TestStdioHelperProcess"}, CWD: t.TempDir(), Env: map[string]string{"MCPGATE_STDIO_HELPER": "1"}, MaxProcesses: 2, MaxInflight: inflight, MaxFrameBytes: 1 << 20, StartupAttempts: 1, Grace: 30 * time.Millisecond}
}

func callFrame(id, name string) ([]byte, []byte) {
	rawID := []byte(`"` + id + `"`)
	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(rawID), "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}})
	return rawID, frame
}

func TestStdioSharedCancellationDoesNotKillUnrelatedCall(t *testing.T) {
	sup, err := supervisor.New(processConfig(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close(context.Background())
	lease, err := sup.Acquire(context.Background(), supervisor.PartitionKey{Generation: "g", Principal: "p", CredentialEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	id, frame := callFrame("slow", "slow")
	done := make(chan error, 1)
	go func() { _, err := lease.Call(ctx, id, frame); done <- err }()
	id2, frame2 := callFrame("quick", "quick")
	response, err := lease.Call(context.Background(), id2, frame2)
	if err != nil || len(response) == 0 {
		t.Fatalf("unrelated call failed: %s %v", response, err)
	}
	if err := <-done; err == nil {
		t.Fatal("canceled call completed")
	}
}

func TestStdioSingleInflightHangTerminatesOnlyPartition(t *testing.T) {
	sup, err := supervisor.New(processConfig(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close(context.Background())
	lease, err := sup.Acquire(context.Background(), supervisor.PartitionKey{Generation: "g", Principal: "hung", CredentialEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	id, frame := callFrame("hang", "hang")
	if _, err := lease.Call(ctx, id, frame); err == nil {
		t.Fatal("hung call succeeded")
	}
	time.Sleep(80 * time.Millisecond)
	id2, frame2 := callFrame("after", "quick")
	if _, err := lease.Call(context.Background(), id2, frame2); err == nil {
		t.Fatal("terminated single-inflight partition remained usable")
	}
}
