package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcpgate/mcpgate/internal/config"
)

type rotatingKeys struct {
	mu       sync.Mutex
	current  *rsa.PrivateKey
	kid      string
	requests int
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func (keys *rotatingKeys) handler(response http.ResponseWriter, _ *http.Request) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	keys.requests++
	public := keys.current.PublicKey
	payload := map[string]any{"keys": []any{map[string]any{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": keys.kid,
		"n": base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
	}}}
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(payload)
}

func (keys *rotatingKeys) rotate(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys.mu.Lock()
	keys.current = key
	keys.kid = kid
	keys.mu.Unlock()
	return key
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any, extraHeader map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	for name, value := range extraHeader {
		header[name] = value
	}
	encode := func(value any) string {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(body)
	}
	input := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func validClaims(now time.Time, issuer, audience string) map[string]any {
	return map[string]any{
		"iss": issuer, "sub": "person-7", "aud": audience,
		"exp": now.Add(time.Minute).Unix(), "nbf": now.Add(-time.Minute).Unix(),
		"tenant": "tenant-a", "scope": "tools:read tools:call", "client_id": "client-3",
	}
}

func newVerifierFixture(t *testing.T) (*Verifier, *rotatingKeys, *rsa.PrivateKey, string, string) {
	t.Helper()
	keys := &rotatingKeys{}
	key := keys.rotate(t, "first")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		keys.handler(recorder, request)
		result := recorder.Result()
		result.Request = request
		result.Body = io.NopCloser(result.Body)
		return result, nil
	})}
	issuer := "https://issuer.example"
	resource := "https://gateway.example/mcp/team"
	verifier, err := NewVerifier(config.Authentication{
		Kind: "oauth2", Issuer: issuer, JWKSURI: "https://issuer.example/jwks",
		Algorithms: []string{"RS256"}, TenantClaim: "tenant", ScopeClaim: "scope",
	}, client, []byte("partition-key-with-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	return verifier, keys, key, issuer, resource
}

func TestJWTVerificationProducesPartitionedPrincipal(t *testing.T) {
	verifier, _, key, issuer, resource := newVerifierFixture(t)
	token := signJWT(t, key, "first", validClaims(time.Now(), issuer, resource), nil)

	principal, err := verifier.Verify(context.Background(), token, resource)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Issuer != issuer || principal.Subject != "person-7" || principal.Tenant != "tenant-a" || principal.ClientID != "client-3" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if strings.Join(principal.Scopes, ",") != "tools:call,tools:read" {
		t.Fatalf("scopes were not normalized: %#v", principal.Scopes)
	}
	if len(principal.PartitionDigest) != 64 {
		t.Fatalf("partition digest must be opaque sha256 hex: %q", principal.PartitionDigest)
	}
}

func TestJWTRejectsInvalidTrustInputs(t *testing.T) {
	verifier, _, key, issuer, resource := newVerifierFixture(t)
	now := time.Now()
	tests := []struct {
		name   string
		claims map[string]any
		header map[string]any
		mutate func(string) string
	}{
		{name: "wrong issuer", claims: validClaims(now, "https://other.example", resource)},
		{name: "wrong audience", claims: validClaims(now, issuer, "https://other.example")},
		{name: "expired", claims: func() map[string]any {
			c := validClaims(now, issuer, resource)
			c["exp"] = now.Add(-time.Minute).Unix()
			return c
		}()},
		{name: "not yet valid", claims: func() map[string]any {
			c := validClaims(now, issuer, resource)
			c["nbf"] = now.Add(time.Minute).Unix()
			return c
		}()},
		{name: "malformed tenant", claims: func() map[string]any {
			c := validClaims(now, issuer, resource)
			c["tenant"] = []string{"tenant-a"}
			return c
		}()},
		{name: "malformed scope", claims: func() map[string]any {
			c := validClaims(now, issuer, resource)
			c["scope"] = map[string]string{"bad": "shape"}
			return c
		}()},
		{name: "token supplied jku", claims: validClaims(now, issuer, resource), header: map[string]any{"jku": "https://attacker.invalid/jwks"}},
		{name: "missing signature", claims: validClaims(now, issuer, resource), mutate: func(token string) string {
			return strings.TrimSuffix(token, token[strings.LastIndex(token, "."):]) + "."
		}},
		{name: "disallowed algorithm", claims: validClaims(now, issuer, resource), mutate: func(token string) string {
			parts := strings.Split(token, ".")
			header, _ := json.Marshal(map[string]any{"alg": "HS256", "kid": "first"})
			parts[0] = base64.RawURLEncoding.EncodeToString(header)
			return strings.Join(parts, ".")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := signJWT(t, key, "first", test.claims, test.header)
			if test.mutate != nil {
				token = test.mutate(token)
			}
			if _, err := verifier.Verify(context.Background(), token, resource); err == nil {
				t.Fatal("expected verification failure")
			}
		})
	}
}

func TestJWKSUnknownKeyTriggersOneRefresh(t *testing.T) {
	verifier, keys, first, issuer, resource := newVerifierFixture(t)
	if _, err := verifier.Verify(context.Background(), signJWT(t, first, "first", validClaims(time.Now(), issuer, resource), nil), resource); err != nil {
		t.Fatal(err)
	}
	second := keys.rotate(t, "second")
	if _, err := verifier.Verify(context.Background(), signJWT(t, second, "second", validClaims(time.Now(), issuer, resource), nil), resource); err != nil {
		t.Fatalf("rotated key was not accepted after refresh: %v", err)
	}
	keys.mu.Lock()
	requests := keys.requests
	keys.mu.Unlock()
	if requests != 2 {
		t.Fatalf("expected initial fetch plus one refresh, got %d", requests)
	}
}

func TestOAuthProtectedResourceMetadataAndChallenge(t *testing.T) {
	metadata := ProtectedResourceMetadata{
		Resource:             "https://gateway.example/mcp/team",
		AuthorizationServers: []string{"https://issuer.example"},
		ScopesSupported:      []string{"tools:read", "tools:call"},
	}
	recorder := httptest.NewRecorder()
	MetadataHandler(metadata).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), metadata.Resource) {
		t.Fatalf("bad metadata response: %d %s", recorder.Code, recorder.Body.String())
	}
	challenge := BearerChallenge(metadata.Resource, "invalid_token", "signature verification failed")
	if challenge != fmt.Sprintf(`Bearer resource=%q, error="invalid_token", error_description="signature verification failed"`, metadata.Resource) {
		t.Fatalf("unexpected challenge: %s", challenge)
	}
}
