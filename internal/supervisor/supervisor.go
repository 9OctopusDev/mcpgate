package supervisor

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Supervisor struct {
	config    Config
	mu        sync.Mutex
	processes map[string]*processEntry
	closed    bool
}
type processEntry struct {
	process    *Process
	references int
}
type Lease struct {
	supervisor *Supervisor
	key        string
	process    *Process
	once       sync.Once
}

func New(config Config) (*Supervisor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Supervisor{config: config, processes: make(map[string]*processEntry)}, nil
}
func (supervisor *Supervisor) Acquire(ctx context.Context, partition PartitionKey) (*Lease, error) {
	key := partition.String()
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return nil, errors.New("supervisor closed")
	}
	if entry := supervisor.processes[key]; entry != nil && entry.process.State() == StateReady {
		entry.references++
		lease := &Lease{supervisor: supervisor, key: key, process: entry.process}
		supervisor.mu.Unlock()
		return lease, nil
	}
	if len(supervisor.processes) >= supervisor.config.MaxProcesses {
		supervisor.mu.Unlock()
		return nil, errors.New("stdio process capacity reached")
	}
	supervisor.mu.Unlock()
	var process *Process
	var err error
	for attempt := 0; attempt < supervisor.config.StartupAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		process, err = startProcess(supervisor.config)
		if err == nil {
			break
		}
		delay := Backoff(attempt, 10*time.Millisecond, time.Second)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return nil, err
	}
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		_ = process.Drain(ctx)
		return nil, errors.New("supervisor closed")
	}
	if existing := supervisor.processes[key]; existing != nil && existing.process.State() == StateReady {
		existing.references++
		supervisor.mu.Unlock()
		_ = process.Drain(ctx)
		return &Lease{supervisor: supervisor, key: key, process: existing.process}, nil
	}
	supervisor.processes[key] = &processEntry{process: process, references: 1}
	supervisor.mu.Unlock()
	return &Lease{supervisor: supervisor, key: key, process: process}, nil
}
func (lease *Lease) Call(ctx context.Context, id []byte, frame []byte) ([]byte, error) {
	return lease.process.Call(ctx, id, frame)
}
func (lease *Lease) Release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		lease.supervisor.mu.Lock()
		if entry := lease.supervisor.processes[lease.key]; entry != nil && entry.references > 0 {
			entry.references--
		}
		lease.supervisor.mu.Unlock()
	})
}
func (supervisor *Supervisor) Close(ctx context.Context) error {
	supervisor.mu.Lock()
	if supervisor.closed {
		supervisor.mu.Unlock()
		return nil
	}
	supervisor.closed = true
	entries := supervisor.processes
	supervisor.processes = make(map[string]*processEntry)
	supervisor.mu.Unlock()
	var first error
	for _, entry := range entries {
		if err := entry.process.Drain(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
