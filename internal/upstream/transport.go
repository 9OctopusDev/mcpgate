package upstream

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"time"
)

func NewTransport(policy NetworkPolicy) *http.Transport {
	transport, err := NewTransportWithDependencies(policy, net.DefaultResolver, &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second})
	if err != nil {
		return nil
	}
	return transport
}

func NewTransportWithDependencies(policy NetworkPolicy, resolver Resolver, dialer ContextDialer) (*http.Transport, error) {
	controlled, err := NewControlledDialer(policy, resolver, dialer)
	if err != nil {
		return nil, err
	}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           controlled.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: controlled.policy.serverName},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
	}, nil
}

func NewClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func FreshRequestHeaders(downstream http.Header) http.Header {
	fresh := make(http.Header)
	allowed := []string{"Accept", "Content-Type", "Traceparent", "Tracestate", "X-Request-ID"}
	for name, values := range downstream {
		for _, permitted := range allowed {
			if !strings.EqualFold(name, permitted) {
				continue
			}
			for _, value := range values {
				fresh.Add(permitted, value)
			}
			break
		}
	}
	return fresh
}
