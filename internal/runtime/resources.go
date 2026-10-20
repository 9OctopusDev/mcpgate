package runtime

import (
	"context"
	"sync"
)

type CloseFunc func(context.Context) error

type SharedResource struct {
	close CloseFunc

	mu       sync.Mutex
	refs     int
	closed   bool
	closeErr error
}

func NewSharedResource(close CloseFunc) *SharedResource {
	return &SharedResource{close: close}
}

func (resource *SharedResource) retain() {
	resource.mu.Lock()
	defer resource.mu.Unlock()
	if resource.closed {
		panic("runtime: retaining closed shared resource")
	}
	resource.refs++
}

func (resource *SharedResource) release(ctx context.Context) {
	resource.mu.Lock()
	if resource.refs > 0 {
		resource.refs--
	}
	shouldClose := resource.refs == 0 && !resource.closed
	if shouldClose {
		resource.closed = true
	}
	resource.mu.Unlock()
	if shouldClose && resource.close != nil {
		err := resource.close(ctx)
		resource.mu.Lock()
		resource.closeErr = err
		resource.mu.Unlock()
	}
}

func (resource *SharedResource) RefCount() int {
	resource.mu.Lock()
	defer resource.mu.Unlock()
	return resource.refs
}

func (resource *SharedResource) Closed() bool {
	resource.mu.Lock()
	defer resource.mu.Unlock()
	return resource.closed
}
