package catalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type testSigningKey struct {
	identity contracts.CredentialIdentity
	key      []byte
}

func (key *testSigningKey) Identity() contracts.CredentialIdentity { return key.identity }
func (key *testSigningKey) ApplyHTTP(*http.Request) error {
	return contracts.ErrCredentialOperationUnsupported
}
func (key *testSigningKey) Sign(message []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, key.key)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}
func (key *testSigningKey) Verify(message, signature []byte) error {
	expected, _ := key.Sign(message)
	if !hmac.Equal(expected, signature) {
		return errors.New("invalid")
	}
	return nil
}
func (key *testSigningKey) Close() {}

type catalogAuthorizer struct {
	epoch       atomic.Uint64
	denied      map[string]bool
	calls       atomic.Int32
	onAuthorize func(int)
}

func (authorizer *catalogAuthorizer) Epoch() uint64 { return authorizer.epoch.Load() }
func (authorizer *catalogAuthorizer) Authorize(_ context.Context, _ contracts.Principal, route contracts.Route) (contracts.Decision, error) {
	call := int(authorizer.calls.Add(1))
	if authorizer.onAuthorize != nil {
		authorizer.onAuthorize(call)
	}
	return contracts.Decision{Allowed: !authorizer.denied[route.Key.PublicName], Epoch: authorizer.epoch.Load()}, nil
}
func (authorizer *catalogAuthorizer) RecheckAtDispatch(context.Context, contracts.Principal, contracts.Route, uint64) (contracts.Decision, error) {
	return contracts.Decision{}, nil
}

func catalogRoutes(names ...string) []contracts.Route {
	routes := make([]contracts.Route, 0, len(names))
	for _, name := range names {
		routes = append(routes, contracts.Route{
			Key:      contracts.RouteKey{ServerID: "team", PublicName: name},
			Contract: contracts.ToolContract{Name: name, Description: "tool " + name, InputSchema: []byte(`{"type":"object"}`), Fingerprint: name + "-fp"},
		})
	}
	return routes
}

func baseListRequest(authorizer *catalogAuthorizer) ListRequest {
	if authorizer.epoch.Load() == 0 {
		authorizer.epoch.Store(1)
	}
	return ListRequest{
		ServerID: "team", Principal: contracts.Principal{PartitionDigest: "partition-a"},
		Routes: catalogRoutes("z.last", "a.first", "m.middle"), Authorizer: authorizer,
		Revision: "catalog-rev-1", PageSize: 10,
		CursorKey:         &testSigningKey{identity: contracts.CredentialIdentity{Reference: "env:CURSOR", Epoch: 4}, key: []byte("01234567890123456789012345678901")},
		Now:               func() time.Time { return time.Unix(2_000_000_000, 0) },
		SupportedFeatures: map[string]bool{"progress": true},
	}
}

func TestAuthorizedCatalogFiltersBeforePaginationAndSorts(t *testing.T) {
	authorizer := &catalogAuthorizer{denied: map[string]bool{"m.middle": true}}
	request := baseListRequest(authorizer)
	request.PageSize = 1
	page, err := List(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tools) != 1 || page.Tools[0].Name != "a.first" || page.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", page)
	}
	if page.TTLMillis != 0 || page.CacheScope != "private" {
		t.Fatalf("unsafe cache hints: %+v", page)
	}
	request.Cursor = page.NextCursor
	page, err = List(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tools) != 1 || page.Tools[0].Name != "z.last" || page.NextCursor != "" {
		t.Fatalf("filtering did not precede pagination: %+v", page)
	}
}

func TestAuthorizedCatalogOmitsUnavailableAndUnsupportedContracts(t *testing.T) {
	authorizer := &catalogAuthorizer{}
	request := baseListRequest(authorizer)
	request.Routes[0].Contract.RequiredFeatures = []string{"interactive"}
	request.Available = func(route contracts.Route) bool { return route.Key.PublicName != "m.middle" }
	page, err := List(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tools) != 1 || page.Tools[0].Name != "a.first" {
		t.Fatalf("effective catalog intersection failed: %+v", page.Tools)
	}
}

func TestEpochChangeRestartsOnceThenReturnsStablePage(t *testing.T) {
	authorizer := &catalogAuthorizer{}
	authorizer.epoch.Store(1)
	authorizer.onAuthorize = func(call int) {
		if call == 1 {
			authorizer.epoch.Store(2)
		}
	}
	request := baseListRequest(authorizer)
	page, err := List(context.Background(), request)
	if err != nil || len(page.Tools) != 3 || page.PolicyEpoch != 2 {
		t.Fatalf("catalog did not restart to stable epoch: page=%+v err=%v", page, err)
	}
	if authorizer.calls.Load() != 4 {
		t.Fatalf("expected one partial attempt and one full retry, authorize calls=%d", authorizer.calls.Load())
	}
}

func TestEpochChangesTwiceFailsAfterTwoAttempts(t *testing.T) {
	authorizer := &catalogAuthorizer{}
	authorizer.epoch.Store(1)
	authorizer.onAuthorize = func(call int) {
		if call == 1 || call == 2 {
			authorizer.epoch.Add(1)
		}
	}
	request := baseListRequest(authorizer)
	request.Routes = catalogRoutes("a.first")
	_, err := List(context.Background(), request)
	if !errors.Is(err, ErrPolicyChanged) {
		t.Fatalf("expected bounded retryable failure, got %v", err)
	}
	if authorizer.calls.Load() != 2 {
		t.Fatalf("attempt count was not bounded at two: %d", authorizer.calls.Load())
	}
}

func TestFingerprintChangesWithPublicContract(t *testing.T) {
	contract := contracts.ToolContract{Name: "a", Description: "first", InputSchema: []byte(`{"type":"object"}`)}
	first := Fingerprint(contract)
	contract.Description = "second"
	second := Fingerprint(contract)
	if first == second || len(first) != 64 || len(second) != 64 {
		t.Fatalf("invalid public contract fingerprints: %q %q", first, second)
	}
}
