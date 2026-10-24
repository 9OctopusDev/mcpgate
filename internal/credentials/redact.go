package credentials

import (
	"fmt"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func RevisionMaterial(binding contracts.CredentialBinding) string {
	identity := binding.Identity()
	return fmt.Sprintf("%s@%d", identity.Reference, identity.Epoch)
}
