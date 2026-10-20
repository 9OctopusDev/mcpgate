package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

const (
	ModernVersion = "2026-07-28"
	LegacyVersion = "2025-11-25"
)

type Limits struct {
	MaxPages         int
	MaxTools         int
	MaxBytes         int64
	DiscoveryTimeout time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxPages: 100, MaxTools: 10000, MaxBytes: 4 << 20, DiscoveryTimeout: 10 * time.Second}
}

type HTTPOptions struct {
	Endpoint   string
	Client     *http.Client
	Credential contracts.CredentialBinding
	Limits     Limits
	Version    string
}

type Adapter struct {
	endpoint   string
	client     *http.Client
	credential contracts.CredentialBinding
	limits     Limits
	mu         sync.RWMutex
	version    string
	catalog    contracts.Catalog
}

func NewHTTPAdapter(options HTTPOptions) (*Adapter, error) {
	if options.Endpoint == "" || options.Client == nil {
		return nil, fmt.Errorf("endpoint and HTTP client are required")
	}
	limits := options.Limits
	if limits.MaxPages <= 0 || limits.MaxTools <= 0 || limits.MaxBytes <= 0 || limits.DiscoveryTimeout <= 0 {
		limits = DefaultLimits()
	}
	client := *options.Client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	version := options.Version
	if version == "" {
		version = ModernVersion
	}
	return &Adapter{endpoint: options.Endpoint, client: &client, credential: options.Credential, limits: limits, version: version}, nil
}

func (adapter *Adapter) Discover(ctx context.Context, _ contracts.CredentialBinding) (contracts.Catalog, error) {
	result, err := adapter.DetectAndDiscover(ctx)
	return result.Catalog, err
}

func (adapter *Adapter) Close(context.Context) error {
	adapter.client.CloseIdleConnections()
	return nil
}
func newRequestID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}

var _ contracts.ToolAdapter = (*Adapter)(nil)
