package runtime

import (
	"sync"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type Lease struct {
	store *Store
	state *generationState
	once  sync.Once
}

func (lease *Lease) Generation() *Generation {
	if lease == nil || lease.state == nil {
		return nil
	}
	return lease.state.generation
}

func (lease *Lease) Snapshot() contracts.Snapshot { return lease.Generation() }

func (lease *Lease) Release() {
	if lease == nil || lease.store == nil || lease.state == nil {
		return
	}
	lease.once.Do(func() { lease.store.release(lease.state) })
}
