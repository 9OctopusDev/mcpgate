package executor

import "sync"

type CorrelationKey struct {
	Principal string
	Session   string
	Request   string
}

type Correlations struct {
	mu      sync.Mutex
	entries map[CorrelationKey]map[string]func()
}

func NewCorrelations() *Correlations {
	return &Correlations{entries: make(map[CorrelationKey]map[string]func())}
}

func (registry *Correlations) Track(key CorrelationKey, execution string, cancel func()) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.entries[key] == nil {
		registry.entries[key] = make(map[string]func())
	}
	registry.entries[key][execution] = cancel
}

func (registry *Correlations) Untrack(key CorrelationKey, execution string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.entries[key], execution)
	if len(registry.entries[key]) == 0 {
		delete(registry.entries, key)
	}
}

func (registry *Correlations) Cancel(key CorrelationKey) bool {
	registry.mu.Lock()
	cancels := registry.entries[key]
	delete(registry.entries, key)
	registry.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels) > 0
}
