package identity

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mcpgate/mcpgate/internal/config"
	"github.com/mcpgate/mcpgate/internal/contracts"
)

type Verifier struct {
	configuration config.Authentication
	client        *http.Client
	partitionKey  []byte
	algorithms    map[string]struct{}

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

type protectedHeader struct {
	Algorithm string          `json:"alg"`
	Type      string          `json:"typ,omitempty"`
	KeyID     string          `json:"kid"`
	JKU       json.RawMessage `json:"jku,omitempty"`
	X5U       json.RawMessage `json:"x5u,omitempty"`
	X5C       json.RawMessage `json:"x5c,omitempty"`
	Critical  json.RawMessage `json:"crit,omitempty"`
	B64       json.RawMessage `json:"b64,omitempty"`
}

func NewVerifier(configuration config.Authentication, client *http.Client, partitionKey []byte) (*Verifier, error) {
	if configuration.Issuer == "" || configuration.JWKSURI == "" || configuration.TenantClaim == "" || configuration.ScopeClaim == "" {
		return nil, fmt.Errorf("%w: incomplete verifier configuration", ErrIdentityVerification)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if len(partitionKey) < 32 {
		return nil, fmt.Errorf("%w: partition key too short", ErrIdentityVerification)
	}
	algorithms := make(map[string]struct{}, len(configuration.Algorithms))
	for _, algorithm := range configuration.Algorithms {
		if algorithm != "RS256" {
			return nil, fmt.Errorf("%w: unsupported configured algorithm", ErrIdentityVerification)
		}
		algorithms[algorithm] = struct{}{}
	}
	if len(algorithms) == 0 {
		return nil, fmt.Errorf("%w: algorithm allowlist is empty", ErrIdentityVerification)
	}
	return &Verifier{configuration: configuration, client: client, partitionKey: append([]byte(nil), partitionKey...), algorithms: algorithms}, nil
}

func (verifier *Verifier) Verify(ctx context.Context, rawToken, resourceURI string) (contracts.Principal, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return contracts.Principal{}, fmt.Errorf("%w: malformed signed token", ErrIdentityVerification)
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return contracts.Principal{}, fmt.Errorf("%w: malformed protected header", ErrIdentityVerification)
	}
	var header protectedHeader
	decoder := json.NewDecoder(strings.NewReader(string(headerBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil || header.Algorithm == "" || header.KeyID == "" || len(header.JKU)+len(header.X5U)+len(header.X5C)+len(header.Critical)+len(header.B64) != 0 {
		return contracts.Principal{}, fmt.Errorf("%w: untrusted protected header", ErrIdentityVerification)
	}
	if _, allowed := verifier.algorithms[header.Algorithm]; !allowed {
		return contracts.Principal{}, fmt.Errorf("%w: disallowed token algorithm", ErrIdentityVerification)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return contracts.Principal{}, fmt.Errorf("%w: malformed signature", ErrIdentityVerification)
	}
	key, ok, err := verifier.key(ctx, header.KeyID, false)
	if err != nil {
		return contracts.Principal{}, err
	}
	if !ok || verifyRS256(key, []byte(parts[0]+"."+parts[1]), signature) != nil {
		key, ok, err = verifier.key(ctx, header.KeyID, true)
		if err != nil || !ok || verifyRS256(key, []byte(parts[0]+"."+parts[1]), signature) != nil {
			return contracts.Principal{}, fmt.Errorf("%w: signature verification failed", ErrIdentityVerification)
		}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return contracts.Principal{}, fmt.Errorf("%w: malformed claims", ErrIdentityVerification)
	}
	var claims map[string]any
	claimDecoder := json.NewDecoder(strings.NewReader(string(payload)))
	claimDecoder.UseNumber()
	if err := claimDecoder.Decode(&claims); err != nil {
		return contracts.Principal{}, fmt.Errorf("%w: malformed claims", ErrIdentityVerification)
	}
	return verifier.principal(claims, resourceURI)
}

func verifyRS256(key *rsa.PublicKey, input, signature []byte) error {
	digest := sha256.Sum256(input)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature)
}

func (verifier *Verifier) key(ctx context.Context, keyID string, refresh bool) (*rsa.PublicKey, bool, error) {
	verifier.mu.RLock()
	key, ok := verifier.keys[keyID]
	loaded := verifier.keys != nil
	verifier.mu.RUnlock()
	if ok && !refresh {
		return key, true, nil
	}
	if !loaded || refresh {
		keys, err := fetchKeys(ctx, verifier.client, verifier.configuration.JWKSURI)
		if err != nil {
			return nil, false, err
		}
		verifier.mu.Lock()
		verifier.keys = keys
		key, ok = keys[keyID]
		verifier.mu.Unlock()
	}
	return key, ok, nil
}

func (verifier *Verifier) principal(claims map[string]any, resourceURI string) (contracts.Principal, error) {
	issuer, ok := stringClaim(claims, "iss")
	if !ok || issuer != verifier.configuration.Issuer {
		return contracts.Principal{}, fmt.Errorf("%w: issuer mismatch", ErrIdentityVerification)
	}
	subject, ok := stringClaim(claims, "sub")
	if !ok {
		return contracts.Principal{}, fmt.Errorf("%w: subject missing", ErrIdentityVerification)
	}
	if !audienceContains(claims["aud"], resourceURI) {
		return contracts.Principal{}, fmt.Errorf("%w: audience mismatch", ErrIdentityVerification)
	}
	now := time.Now()
	expires, ok := numericDate(claims["exp"])
	if !ok || !expires.After(now) {
		return contracts.Principal{}, fmt.Errorf("%w: expired token", ErrIdentityVerification)
	}
	if raw, exists := claims["nbf"]; exists {
		notBefore, valid := numericDate(raw)
		if !valid || notBefore.After(now) {
			return contracts.Principal{}, fmt.Errorf("%w: token not active", ErrIdentityVerification)
		}
	}
	tenant, ok := stringClaim(claims, verifier.configuration.TenantClaim)
	if !ok {
		return contracts.Principal{}, fmt.Errorf("%w: malformed tenant claim", ErrIdentityVerification)
	}
	scopes, ok := scopeClaim(claims[verifier.configuration.ScopeClaim])
	if !ok {
		return contracts.Principal{}, fmt.Errorf("%w: malformed scope claim", ErrIdentityVerification)
	}
	clientID, _ := stringClaim(claims, "client_id")
	if clientID == "" {
		clientID, _ = stringClaim(claims, "azp")
	}
	return makePrincipal(verifier.partitionKey, issuer, subject, tenant, clientID, scopes), nil
}

func stringClaim(claims map[string]any, name string) (string, bool) {
	value, ok := claims[name].(string)
	return value, ok && value != ""
}

func numericDate(raw any) (time.Time, bool) {
	number, ok := raw.(json.Number)
	if !ok {
		return time.Time{}, false
	}
	seconds, err := number.Int64()
	return time.Unix(seconds, 0), err == nil
}

func audienceContains(raw any, resource string) bool {
	switch value := raw.(type) {
	case string:
		return value == resource
	case []any:
		for _, candidate := range value {
			if text, ok := candidate.(string); ok && text == resource {
				return true
			}
		}
	}
	return false
}

func scopeClaim(raw any) ([]string, bool) {
	seen := make(map[string]struct{})
	var scopes []string
	add := func(scope string) bool {
		if scope == "" || strings.ContainsAny(scope, "\r\n\t") {
			return false
		}
		if _, exists := seen[scope]; !exists {
			seen[scope] = struct{}{}
			scopes = append(scopes, scope)
		}
		return true
	}
	switch value := raw.(type) {
	case string:
		for _, scope := range strings.Fields(value) {
			if !add(scope) {
				return nil, false
			}
		}
	case []any:
		for _, item := range value {
			scope, ok := item.(string)
			if !ok || !add(scope) {
				return nil, false
			}
		}
	default:
		return nil, false
	}
	return scopes, len(scopes) > 0
}
