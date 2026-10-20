package modern

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const ProtocolVersion = "2026-07-28"

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type Metadata struct {
	ProtocolVersion    string          `json:"io.modelcontextprotocol/protocolVersion"`
	ClientInfo         ClientInfo      `json:"io.modelcontextprotocol/clientInfo"`
	ClientCapabilities json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
}

func validateMetadata(request *http.Request, metadata Metadata) error {
	if metadata.ProtocolVersion != ProtocolVersion || request.Header.Get("MCP-Protocol-Version") != metadata.ProtocolVersion {
		return fmt.Errorf("protocol version metadata mismatch")
	}
	if metadata.ClientInfo.Name == "" || metadata.ClientInfo.Version == "" || len(metadata.ClientCapabilities) == 0 {
		return fmt.Errorf("required client metadata missing")
	}
	if request.Header.Get("MCP-Client-Name") != metadata.ClientInfo.Name || request.Header.Get("MCP-Client-Version") != metadata.ClientInfo.Version {
		return fmt.Errorf("client metadata mismatch")
	}
	return nil
}
