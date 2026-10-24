package compiler

import (
	"github.com/9OctopusDev/mcpgate/internal/config"
	"github.com/9OctopusDev/mcpgate/internal/contracts"
	gatewayruntime "github.com/9OctopusDev/mcpgate/internal/runtime"
)

type Inputs struct {
	Document             *config.Document
	CredentialIdentities map[string]contracts.CredentialIdentity
	Manifests            map[string]contracts.Catalog
	SecurityResolved     map[string]bool
	Resources            []*gatewayruntime.SharedResource
}
