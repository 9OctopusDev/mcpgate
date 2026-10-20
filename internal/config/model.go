package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value []byte) error {
	text := strings.Trim(strings.TrimSpace(string(value)), `"'`)
	parsed, err := time.ParseDuration(text)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("duration must be positive: %q", text)
	}
	d.Duration = parsed
	return nil
}

func (d *Duration) UnmarshalJSON(value []byte) error {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return err
	}
	return d.UnmarshalYAML([]byte(text))
}

type Document struct {
	APIVersion     string                    `yaml:"api_version" json:"api_version"`
	Gateway        Gateway                   `yaml:"gateway" json:"gateway"`
	Secrets        map[string]SecretRef      `yaml:"secrets" json:"secrets"`
	Authentication map[string]Authentication `yaml:"authentication" json:"authentication"`
	Policies       map[string]Policy         `yaml:"policies" json:"policies"`
	Upstreams      map[string]Upstream       `yaml:"upstreams" json:"upstreams"`
	Servers        map[string]Server         `yaml:"servers" json:"servers"`
	Observability  Observability             `yaml:"observability" json:"observability"`
	Source         SourceRef                 `yaml:"-" json:"-"`
}

type Gateway struct {
	Name      string        `yaml:"name" json:"name"`
	Data      DataListener  `yaml:"data" json:"data"`
	Admin     AdminListener `yaml:"admin" json:"admin"`
	Limits    Limits        `yaml:"limits" json:"limits"`
	Lifecycle Lifecycle     `yaml:"lifecycle" json:"lifecycle"`
}

type DataListener struct {
	Listen             string   `yaml:"listen" json:"listen"`
	AllowedHosts       []string `yaml:"allowed_hosts" json:"allowed_hosts"`
	AllowedOrigins     []string `yaml:"allowed_origins" json:"allowed_origins"`
	AllowMissingOrigin bool     `yaml:"allow_missing_origin" json:"allow_missing_origin"`
}

type AdminListener struct {
	Listen string `yaml:"listen" json:"listen"`
}

type Limits struct {
	MaxInflight       int      `yaml:"max_inflight" json:"max_inflight"`
	MaxQueued         int      `yaml:"max_queued" json:"max_queued"`
	QueueTimeout      Duration `yaml:"queue_timeout" json:"queue_timeout"`
	CallTimeout       Duration `yaml:"call_timeout" json:"call_timeout"`
	RequestBytes      int64    `yaml:"request_bytes" json:"request_bytes"`
	ResponseBytes     int64    `yaml:"response_bytes" json:"response_bytes"`
	SchemaBytes       int64    `yaml:"schema_bytes" json:"schema_bytes"`
	BufferBudgetBytes int64    `yaml:"buffer_budget_bytes" json:"buffer_budget_bytes"`
	PerPrincipalRPS   int      `yaml:"per_principal_rps" json:"per_principal_rps"`
	PerPrincipalBurst int      `yaml:"per_principal_burst" json:"per_principal_burst"`
}

type Lifecycle struct {
	StartupTimeout    Duration `yaml:"startup_timeout" json:"startup_timeout"`
	DrainTimeout      Duration `yaml:"drain_timeout" json:"drain_timeout"`
	LegacySessionIdle Duration `yaml:"legacy_session_idle" json:"legacy_session_idle"`
	MaxLegacySessions int      `yaml:"max_legacy_sessions" json:"max_legacy_sessions"`
}

type SecretRef struct {
	Source string `yaml:"source" json:"source"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
}

type Authentication struct {
	Kind        string   `yaml:"kind" json:"kind"`
	Issuer      string   `yaml:"issuer" json:"issuer"`
	JWKSURI     string   `yaml:"jwks_uri" json:"jwks_uri"`
	Algorithms  []string `yaml:"algorithms" json:"algorithms"`
	TenantClaim string   `yaml:"tenant_claim" json:"tenant_claim"`
	ScopeClaim  string   `yaml:"scope_claim" json:"scope_claim"`
}

type Policy struct {
	Tenants   []string `yaml:"tenants,omitempty" json:"tenants,omitempty"`
	Subjects  []string `yaml:"subjects,omitempty" json:"subjects,omitempty"`
	ScopesAll []string `yaml:"scopes_all,omitempty" json:"scopes_all,omitempty"`
}

type Upstream struct {
	Kind       string            `yaml:"kind" json:"kind"`
	Required   bool              `yaml:"required" json:"required"`
	Endpoints  []Endpoint        `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Network    Network           `yaml:"network,omitempty" json:"network,omitempty"`
	Auth       UpstreamAuth      `yaml:"auth,omitempty" json:"auth,omitempty"`
	OpenAPI    OpenAPI           `yaml:"openapi,omitempty" json:"openapi,omitempty"`
	Process    Process           `yaml:"process,omitempty" json:"process,omitempty"`
	Protocol   Protocol          `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Discovery  Discovery         `yaml:"discovery,omitempty" json:"discovery,omitempty"`
	Isolation  string            `yaml:"isolation,omitempty" json:"isolation,omitempty"`
	StateModel string            `yaml:"state_model,omitempty" json:"state_model,omitempty"`
	Execution  UpstreamExecution `yaml:"execution" json:"execution"`
}

type Endpoint struct {
	ID     string `yaml:"id" json:"id"`
	URL    string `yaml:"url" json:"url"`
	Weight int    `yaml:"weight" json:"weight"`
}

type Network struct {
	AllowedHosts []string `yaml:"allowed_hosts" json:"allowed_hosts"`
	AllowedCIDRs any      `yaml:"allowed_cidrs" json:"allowed_cidrs"`
}

type UpstreamAuth struct {
	Kind   string `yaml:"kind" json:"kind"`
	Secret string `yaml:"secret" json:"secret"`
}

type OpenAPI struct {
	File         string `yaml:"file" json:"file"`
	ExternalRefs string `yaml:"external_refs" json:"external_refs"`
}

type Process struct {
	Executable    string            `yaml:"executable" json:"executable"`
	Args          []string          `yaml:"args" json:"args"`
	CWD           string            `yaml:"cwd" json:"cwd"`
	Env           map[string]string `yaml:"env" json:"env"`
	MaxProcesses  int               `yaml:"max_processes" json:"max_processes"`
	MaxFrameBytes int64             `yaml:"max_frame_bytes" json:"max_frame_bytes"`
}

type Protocol struct {
	Versions []string `yaml:"versions" json:"versions"`
	Detect   string   `yaml:"detect" json:"detect"`
}

type Discovery struct {
	RefreshInterval Duration `yaml:"refresh_interval" json:"refresh_interval"`
	MaxStale        Duration `yaml:"max_stale" json:"max_stale"`
}

type UpstreamExecution struct {
	Timeout     Duration `yaml:"timeout" json:"timeout"`
	MaxInflight int      `yaml:"max_inflight" json:"max_inflight"`
	MaxAttempts int      `yaml:"max_attempts" json:"max_attempts"`
}

type Server struct {
	Path           string         `yaml:"path" json:"path"`
	ResourceURI    string         `yaml:"resource_uri" json:"resource_uri"`
	Authentication string         `yaml:"authentication" json:"authentication"`
	Protocols      []string       `yaml:"protocols" json:"protocols"`
	Features       ServerFeatures `yaml:"features" json:"features"`
	Catalog        Catalog        `yaml:"catalog" json:"catalog"`
	Exports        []Export       `yaml:"exports" json:"exports"`
}

type ServerFeatures struct {
	Tools             bool `yaml:"tools" json:"tools"`
	ListChanged       bool `yaml:"list_changed" json:"list_changed"`
	InteractiveBridge bool `yaml:"interactive_bridge" json:"interactive_bridge"`
	Resources         bool `yaml:"resources" json:"resources"`
	Prompts           bool `yaml:"prompts" json:"prompts"`
}

type Catalog struct {
	PageSize     int    `yaml:"page_size" json:"page_size"`
	TTLMillis    int    `yaml:"ttl_ms" json:"ttl_ms"`
	CursorSecret string `yaml:"cursor_secret" json:"cursor_secret"`
}

type Export struct {
	Name                        string `yaml:"name" json:"name"`
	Upstream                    string `yaml:"upstream" json:"upstream"`
	Native                      string `yaml:"native" json:"native"`
	Policy                      string `yaml:"policy" json:"policy"`
	ServiceIdentityAcknowledged bool   `yaml:"service_identity_acknowledged" json:"service_identity_acknowledged"`
	Behavior                    string `yaml:"behavior" json:"behavior"`
}

type Observability struct {
	Logs           string `yaml:"logs" json:"logs"`
	PayloadLogging bool   `yaml:"payload_logging" json:"payload_logging"`
	Metrics        bool   `yaml:"metrics" json:"metrics"`
	Traces         bool   `yaml:"traces" json:"traces"`
}
