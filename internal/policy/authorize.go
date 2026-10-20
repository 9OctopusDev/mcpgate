package policy

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/mcpgate/mcpgate/internal/contracts"
)

type Engine struct {
	policies map[string]compiledPolicy
	epoch    atomic.Uint64
	mu       sync.RWMutex
	overlay  overlayState
}

func (engine *Engine) Authorize(ctx context.Context, principal contracts.Principal, route contracts.Route) (contracts.Decision, error) {
	if err := ctx.Err(); err != nil {
		return contracts.Decision{}, err
	}
	return engine.decide(principal, route), nil
}

func (engine *Engine) RecheckAtDispatch(ctx context.Context, principal contracts.Principal, route contracts.Route, _ uint64) (contracts.Decision, error) {
	if err := ctx.Err(); err != nil {
		return contracts.Decision{}, err
	}
	return engine.decide(principal, route), nil
}

func (engine *Engine) decide(principal contracts.Principal, route contracts.Route) contracts.Decision {
	epoch := engine.epoch.Load()
	engine.mu.RLock()
	if _, revoked := engine.overlay.principals[principal.PartitionDigest]; revoked {
		engine.mu.RUnlock()
		return contracts.Decision{Epoch: epoch, Reason: "principal revoked"}
	}
	if _, revoked := engine.overlay.exports[route.Key]; revoked {
		engine.mu.RUnlock()
		return contracts.Decision{Epoch: epoch, Reason: "export revoked"}
	}
	if _, revoked := engine.overlay.upstreams[route.UpstreamID]; revoked {
		engine.mu.RUnlock()
		return contracts.Decision{Epoch: epoch, Reason: "upstream revoked"}
	}
	engine.mu.RUnlock()

	policy, exists := engine.policies[route.PolicyID]
	if !exists {
		return contracts.Decision{Epoch: epoch, Reason: "policy not found"}
	}
	if len(policy.tenants) == 0 && len(policy.subjects) == 0 && len(policy.scopes) == 0 {
		return contracts.Decision{Epoch: epoch, Reason: "empty policy denies"}
	}
	if len(policy.tenants) > 0 {
		if _, allowed := policy.tenants[principal.Tenant]; !allowed {
			return contracts.Decision{Epoch: epoch, Reason: "tenant denied"}
		}
	}
	if len(policy.subjects) > 0 {
		if _, allowed := policy.subjects[principal.Subject]; !allowed {
			return contracts.Decision{Epoch: epoch, Reason: "subject denied"}
		}
	}
	if len(policy.scopes) > 0 {
		actual := makeSet(principal.Scopes)
		for required := range policy.scopes {
			if _, present := actual[required]; !present {
				return contracts.Decision{Epoch: epoch, Reason: "scope denied"}
			}
		}
	}
	return contracts.Decision{Allowed: true, Epoch: epoch, Reason: "allowed"}
}
