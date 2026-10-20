package upstream

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type ControlledDialer struct {
	policy   compiledNetworkPolicy
	resolver Resolver
	dialer   ContextDialer
}

func NewControlledDialer(policy NetworkPolicy, resolver Resolver, dialer ContextDialer) (*ControlledDialer, error) {
	compiled, err := compileNetworkPolicy(policy)
	if err != nil {
		return nil, err
	}
	if resolver == nil || dialer == nil {
		return nil, fmt.Errorf("resolver and dialer are required")
	}
	return &ControlledDialer{policy: compiled, resolver: resolver, dialer: dialer}, nil
}

func (dialer *ControlledDialer) ValidateEndpoint(ctx context.Context, host string) error {
	_, err := dialer.resolveAllowed(ctx, host)
	return err
}

func (dialer *ControlledDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream address: %w", err)
	}
	addresses, err := dialer.resolveAllowed(ctx, host)
	if err != nil {
		return nil, err
	}
	return dialer.dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

func (dialer *ControlledDialer) resolveAllowed(ctx context.Context, host string) ([]netip.Addr, error) {
	if !dialer.policy.allowsHost(host) {
		return nil, fmt.Errorf("upstream hostname is not allowed")
	}
	resolved, err := dialer.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(resolved) == 0 {
		return nil, fmt.Errorf("upstream hostname resolution failed")
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, candidate := range resolved {
		address, ok := netip.AddrFromSlice(candidate.IP)
		if !ok || !dialer.policy.allowsAddress(address) {
			return nil, fmt.Errorf("resolved upstream address is not allowed")
		}
		addresses = append(addresses, address.Unmap())
	}
	return addresses, nil
}
