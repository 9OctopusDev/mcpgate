package faults_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/9OctopusDev/mcpgate/internal/upstream"
)

type faultResolver struct{ addresses []net.IPAddr }

func (resolver faultResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, nil
}

type rejectingDialer struct{ called bool }

func (dialer *rejectingDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	dialer.called = true
	return nil, errors.New("should not dial")
}

func TestNetworkDNSChangeToPrivateAddressIsBlocked(t *testing.T) {
	dialer := &rejectingDialer{}
	transport, err := upstream.NewTransportWithDependencies(upstream.NetworkPolicy{ServerName: "api.example", AllowedHosts: []string{"api.example"}, PublicOnly: true}, faultResolver{addresses: []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}}, dialer)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.DialContext(context.Background(), "tcp", "api.example:443")
	if err == nil || dialer.called {
		t.Fatalf("err=%v dialed=%v", err, dialer.called)
	}
}
