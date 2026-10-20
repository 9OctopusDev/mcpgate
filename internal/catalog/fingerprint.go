package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func Fingerprint(contract contracts.ToolContract) string {
	features := append([]string(nil), contract.RequiredFeatures...)
	sort.Strings(features)
	normalize := func(raw json.RawMessage) json.RawMessage {
		if len(raw) == 0 {
			return nil
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			return compact.Bytes()
		}
		return append(json.RawMessage(nil), raw...)
	}
	encoded, _ := json.Marshal(struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Input       json.RawMessage `json:"input,omitempty"`
		Output      json.RawMessage `json:"output,omitempty"`
		Features    []string        `json:"features,omitempty"`
	}{contract.Name, contract.Description, normalize(contract.InputSchema), normalize(contract.OutputSchema), features})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
