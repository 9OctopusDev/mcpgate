package catalog

import (
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func FuzzCursorDecode(f *testing.F) {
	key := &testSigningKey{identity: contracts.CredentialIdentity{Reference: "fuzz", Epoch: 1}, key: []byte("01234567890123456789012345678901")}
	f.Add("")
	f.Add("invalid.invalid")
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > MaxCursorBytes+1 {
			return
		}
		_, _ = decodeCursor(value, key, "server", "partition", "revision", time.Unix(1, 0))
	})
}
