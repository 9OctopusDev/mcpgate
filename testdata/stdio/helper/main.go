package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for scanner.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			fmt.Fprintln(os.Stdout, "not-json")
			writer.Flush()
			continue
		}
		if request.Method == "notifications/cancelled" {
			continue
		}
		if request.Params.Name == "hang" {
			time.Sleep(time.Hour)
			continue
		}
		if request.Params.Name == "stderr" {
			for index := 0; index < 1000; index++ {
				fmt.Fprintln(os.Stderr, "redacted child diagnostic")
			}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}, "structuredContent": map[string]any{"ok": true}}}
		encoded, _ := json.Marshal(response)
		fmt.Fprintln(writer, string(encoded))
		writer.Flush()
	}
}
