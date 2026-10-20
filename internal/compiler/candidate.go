package compiler

import (
	"github.com/mcpgate/mcpgate/internal/config"
	"github.com/mcpgate/mcpgate/internal/contracts"
)

type Inputs struct {
	Document             *config.Document
	CredentialIdentities map[string]contracts.CredentialIdentity
	Manifests            map[string]contracts.Catalog
	SecurityResolved     map[string]bool
}
