package upstream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
)

type scriptedResolver struct {
	mu      sync.Mutex
	answers [][]net.IPAddr
	calls   int
}

func (resolver *scriptedResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.calls >= len(resolver.answers) {
		return nil, errors.New("no scripted DNS answer")
	}
	answer := resolver.answers[resolver.calls]
	resolver.calls++
	return answer, nil
}

type recordingDialer struct {
	mu        sync.Mutex
	addresses []string
}

func (dialer *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.addresses = append(dialer.addresses, address)
	dialer.mu.Unlock()
	return nil, errors.New("recording dialer stops after capture")
}

func TestDNSRebindingBlockedAtDialTime(t *testing.T) {
	resolver := &scriptedResolver{answers: [][]net.IPAddr{
		{{IP: net.ParseIP("93.184.216.34")}},
		{{IP: net.ParseIP("169.254.169.254")}},
	}}
	dialer := &recordingDialer{}
	policy := NetworkPolicy{ServerName: "api.example", AllowedHosts: []string{"api.example"}, PublicOnly: true}
	controlled, err := NewControlledDialer(policy, resolver, dialer)
	if err != nil {
		t.Fatal(err)
	}
	if err := controlled.ValidateEndpoint(context.Background(), "api.example"); err != nil {
		t.Fatalf("preparation should accept initial public DNS result: %v", err)
	}
	if _, err := controlled.DialContext(context.Background(), "tcp", "api.example:443"); err == nil {
		t.Fatal("expected link-local rebinding to be rejected")
	}
	if len(dialer.addresses) != 0 {
		t.Fatalf("connection attempted after rebinding: %#v", dialer.addresses)
	}
}

func TestDNSAllowedAddressIsDialedDirectlyWithTLSHostname(t *testing.T) {
	resolver := &scriptedResolver{answers: [][]net.IPAddr{{{IP: net.ParseIP("93.184.216.34")}}}}
	dialer := &recordingDialer{}
	policy := NetworkPolicy{ServerName: "api.example", AllowedHosts: []string{"api.example"}, PublicOnly: true}
	transport, err := NewTransportWithDependencies(policy, resolver, dialer)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = transport.DialContext(context.Background(), "tcp", "api.example:443")
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "93.184.216.34:443" {
		t.Fatalf("validated address was not dialed directly: %#v", dialer.addresses)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.ServerName != "api.example" {
		t.Fatalf("TLS hostname was not preserved: %#v", transport.TLSClientConfig)
	}
	if transport.Proxy != nil {
		t.Fatal("environment proxy must be disabled")
	}
}

func TestNetworkPolicyExplicitCIDRAndClientIsolation(t *testing.T) {
	policy := NetworkPolicy{
		ServerName: "api.internal", AllowedHosts: []string{"api.internal"},
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
	}
	resolver := &scriptedResolver{answers: [][]net.IPAddr{{{IP: net.ParseIP("10.20.8.9")}}}}
	dialer := &recordingDialer{}
	transport, err := NewTransportWithDependencies(policy, resolver, dialer)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = transport.DialContext(context.Background(), "tcp", "api.internal:8443")
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "10.20.8.9:8443" {
		t.Fatalf("explicit private CIDR was not honored: %#v", dialer.addresses)
	}
	client := NewClient(transport)
	if client.Jar != nil {
		t.Fatal("upstream client must not retain cookies")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://elsewhere.example/", nil)
	via := []*http.Request{{URL: request.URL}}
	if err := client.CheckRedirect(request, via); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirects must be disabled: %v", err)
	}
}

func TestFreshRequestHeadersDropAmbientAuthority(t *testing.T) {
	downstream := http.Header{
		"Authorization": []string{"Bearer caller"}, "Cookie": []string{"session=x"},
		"Host": []string{"gateway.example"}, "Forwarded": []string{"for=127.0.0.1"},
		"Connection": []string{"keep-alive"}, "X-Request-ID": []string{"req-1"},
	}
	upstream := FreshRequestHeaders(downstream)
	for _, forbidden := range []string{"Authorization", "Cookie", "Host", "Forwarded", "Connection"} {
		if upstream.Get(forbidden) != "" {
			t.Fatalf("%s leaked upstream", forbidden)
		}
	}
	if upstream.Get("X-Request-ID") != "req-1" {
		t.Fatalf("safe correlation header missing: %#v", upstream)
	}
	downstream.Set("X-Request-ID", "changed")
	if upstream.Get("X-Request-ID") != "req-1" {
		t.Fatal("upstream headers alias downstream storage")
	}
}
