package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type revisionInput struct {
	APIVersion  string               `json:"api_version"`
	Config      json.RawMessage      `json:"config"`
	Credentials []credentialRevision `json:"credentials"`
	Manifests   []manifestRevision   `json:"manifests"`
	Routes      []routeRevision      `json:"routes"`
}

type credentialRevision struct {
	Name, Reference string
	Epoch           uint64
}
type manifestRevision struct{ Upstream, Revision string }
type routeRevision struct {
	Server, Public, Upstream, Native, Policy, Fingerprint, Behavior string
	TimeoutNanos                                                    int64
	MaxAttempts                                                     int
}

func computeRevision(inputs Inputs, routes map[contracts.RouteKey]contracts.Route) string {
	canonicalConfig, _ := json.Marshal(inputs.Document)
	value := revisionInput{APIVersion: inputs.Document.APIVersion, Config: canonicalConfig}
	for name, identity := range inputs.CredentialIdentities {
		value.Credentials = append(value.Credentials, credentialRevision{Name: name, Reference: identity.Reference, Epoch: identity.Epoch})
	}
	sort.Slice(value.Credentials, func(i, j int) bool { return value.Credentials[i].Name < value.Credentials[j].Name })
	for upstream, manifest := range inputs.Manifests {
		value.Manifests = append(value.Manifests, manifestRevision{Upstream: upstream, Revision: manifest.Revision})
	}
	sort.Slice(value.Manifests, func(i, j int) bool { return value.Manifests[i].Upstream < value.Manifests[j].Upstream })
	for key, route := range routes {
		value.Routes = append(value.Routes, routeRevision{
			Server: key.ServerID, Public: key.PublicName, Upstream: route.UpstreamID, Native: route.NativeName,
			Policy: route.PolicyID, Fingerprint: route.Contract.Fingerprint, Behavior: string(route.Behavior),
			TimeoutNanos: route.Timeout.Nanoseconds(), MaxAttempts: route.MaxAttempts,
		})
	}
	sort.Slice(value.Routes, func(i, j int) bool {
		if value.Routes[i].Server != value.Routes[j].Server {
			return value.Routes[i].Server < value.Routes[j].Server
		}
		return value.Routes[i].Public < value.Routes[j].Public
	})
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
