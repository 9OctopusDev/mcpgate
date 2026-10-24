package policy

import (
	"fmt"

	"github.com/9OctopusDev/mcpgate/internal/config"
)

type compiledPolicy struct {
	tenants  map[string]struct{}
	subjects map[string]struct{}
	scopes   map[string]struct{}
}

func Compile(configuration map[string]config.Policy) (*Engine, error) {
	policies := make(map[string]compiledPolicy, len(configuration))
	for name, declaration := range configuration {
		if name == "" {
			return nil, fmt.Errorf("policy name must not be empty")
		}
		compiled := compiledPolicy{
			tenants: makeSet(declaration.Tenants), subjects: makeSet(declaration.Subjects), scopes: makeSet(declaration.ScopesAll),
		}
		policies[name] = compiled
	}
	engine := &Engine{policies: policies, overlay: newOverlayState()}
	engine.epoch.Store(1)
	return engine, nil
}

func makeSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}
