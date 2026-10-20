// Package schemas embeds mcpgate's versioned configuration schemas into the
// standalone binary.
package schemas

import _ "embed"

// GatewayConfigV1 is the authoritative structural schema for mcpgate/v1.
//
//go:embed gateway-config.v1.schema.json
var GatewayConfigV1 []byte
