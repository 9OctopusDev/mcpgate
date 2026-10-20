package credentials

import (
	"fmt"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

func RevisionMaterial(binding contracts.CredentialBinding) string {
	identity := binding.Identity()
	return fmt.Sprintf("%s@%d", identity.Reference, identity.Epoch)
}
