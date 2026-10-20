package upstream

import (
	"fmt"
	"net/netip"
	"strings"
)

type NetworkPolicy struct {
	ServerName   string
	AllowedHosts []string
	AllowedCIDRs []netip.Prefix
	PublicOnly   bool
}

type compiledNetworkPolicy struct {
	serverName string
	hosts      map[string]struct{}
	cidrs      []netip.Prefix
	publicOnly bool
}

func compileNetworkPolicy(policy NetworkPolicy) (compiledNetworkPolicy, error) {
	serverName := canonicalHost(policy.ServerName)
	if serverName == "" {
		return compiledNetworkPolicy{}, fmt.Errorf("TLS server name is required")
	}
	hosts := make(map[string]struct{}, len(policy.AllowedHosts))
	for _, host := range policy.AllowedHosts {
		canonical := canonicalHost(host)
		if canonical == "" || strings.ContainsAny(canonical, "/*?") {
			return compiledNetworkPolicy{}, fmt.Errorf("invalid allowed hostname %q", host)
		}
		hosts[canonical] = struct{}{}
	}
	if _, ok := hosts[serverName]; !ok {
		return compiledNetworkPolicy{}, fmt.Errorf("TLS server name is not in the host allowlist")
	}
	if !policy.PublicOnly && len(policy.AllowedCIDRs) == 0 {
		return compiledNetworkPolicy{}, fmt.Errorf("destination address policy is empty")
	}
	return compiledNetworkPolicy{
		serverName: serverName, hosts: hosts,
		cidrs: append([]netip.Prefix(nil), policy.AllowedCIDRs...), publicOnly: policy.PublicOnly,
	}, nil
}

func canonicalHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func (policy compiledNetworkPolicy) allowsHost(host string) bool {
	_, allowed := policy.hosts[canonicalHost(host)]
	return allowed
}

func (policy compiledNetworkPolicy) allowsAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return false
	}
	for _, prefix := range policy.cidrs {
		if prefix.Contains(address) {
			return true
		}
	}
	return policy.publicOnly && address.IsGlobalUnicast() && !address.IsPrivate()
}
