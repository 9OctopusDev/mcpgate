package runtime

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Candidate struct {
	Generation       *Generation
	ListenerTopology string
}
type Builder interface {
	Build(context.Context) (Candidate, error)
}
type Status struct {
	Ready     bool      `json:"ready"`
	Reason    string    `json:"reason,omitempty"`
	Revision  string    `json:"revision,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

type CoordinatorOptions struct {
	Store           *Store
	Builder         Builder
	Activate        func(context.Context, Candidate) error
	StopAdmission   func()
	CancelControl   func()
	CloseResources  func(context.Context) error
	InitialTopology string
	ShutdownTimeout time.Duration
}

type Coordinator struct {
	options   CoordinatorOptions
	operation sync.Mutex
	statusMu  sync.RWMutex
	status    Status
	topology  string
	shutdown  sync.Once
}

func NewCoordinator(options CoordinatorOptions) (*Coordinator, error) {
	if options.Store == nil || options.Builder == nil || options.ShutdownTimeout <= 0 {
		return nil, errors.New("runtime coordinator dependencies are incomplete")
	}
	return &Coordinator{options: options, status: Status{Reason: "startup pending"}, topology: options.InitialTopology}, nil
}

func (coordinator *Coordinator) Status() Status {
	coordinator.statusMu.RLock()
	defer coordinator.statusMu.RUnlock()
	return coordinator.status
}
func (coordinator *Coordinator) setStatus(status Status) {
	coordinator.statusMu.Lock()
	coordinator.status = status
	coordinator.statusMu.Unlock()
}

func (coordinator *Coordinator) Start(ctx context.Context) error {
	coordinator.operation.Lock()
	defer coordinator.operation.Unlock()
	coordinator.setStatus(Status{Reason: "preparing runtime"})
	candidate, err := coordinator.options.Builder.Build(ctx)
	if err != nil {
		coordinator.setStatus(Status{Reason: "startup failed"})
		return err
	}
	if candidate.Generation == nil {
		coordinator.setStatus(Status{Reason: "startup candidate invalid"})
		return ErrInvalidGeneration
	}
	if coordinator.options.Activate != nil {
		if err := coordinator.options.Activate(ctx, candidate); err != nil {
			disposeGeneration(candidate.Generation)
			coordinator.setStatus(Status{Reason: "listener activation failed"})
			return err
		}
	}
	if err := coordinator.options.Store.Publish(ctx, candidate.Generation); err != nil {
		coordinator.setStatus(Status{Reason: "generation activation failed"})
		return err
	}
	coordinator.topology = candidate.ListenerTopology
	coordinator.setStatus(Status{Ready: true, Revision: candidate.Generation.Revision(), StartedAt: time.Now().UTC()})
	return nil
}
