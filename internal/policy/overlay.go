package policy

import "github.com/mcpgate/mcpgate/internal/contracts"

type Overlay struct {
	Principals []string
	Exports    []contracts.RouteKey
	Upstreams  []string
}

type overlayState struct {
	principals map[string]struct{}
	exports    map[contracts.RouteKey]struct{}
	upstreams  map[string]struct{}
}

func newOverlayState() overlayState {
	return overlayState{
		principals: make(map[string]struct{}),
		exports:    make(map[contracts.RouteKey]struct{}),
		upstreams:  make(map[string]struct{}),
	}
}

func (engine *Engine) ReplaceOverlay(overlay Overlay) uint64 {
	next := newOverlayState()
	for _, digest := range overlay.Principals {
		if digest != "" {
			next.principals[digest] = struct{}{}
		}
	}
	for _, key := range overlay.Exports {
		if key.ServerID != "" && key.PublicName != "" {
			next.exports[key] = struct{}{}
		}
	}
	for _, upstream := range overlay.Upstreams {
		if upstream != "" {
			next.upstreams[upstream] = struct{}{}
		}
	}
	engine.mu.Lock()
	engine.overlay = next
	engine.mu.Unlock()
	return engine.epoch.Add(1)
}

func (engine *Engine) RevokePrincipal(digest string) uint64 {
	engine.mu.Lock()
	engine.overlay.principals[digest] = struct{}{}
	engine.mu.Unlock()
	return engine.epoch.Add(1)
}

func (engine *Engine) RevokeExport(key contracts.RouteKey) uint64 {
	engine.mu.Lock()
	engine.overlay.exports[key] = struct{}{}
	engine.mu.Unlock()
	return engine.epoch.Add(1)
}

func (engine *Engine) RevokeUpstream(upstream string) uint64 {
	engine.mu.Lock()
	engine.overlay.upstreams[upstream] = struct{}{}
	engine.mu.Unlock()
	return engine.epoch.Add(1)
}

func (engine *Engine) Epoch() uint64 { return engine.epoch.Load() }
