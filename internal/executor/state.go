package executor

import (
	"fmt"
	"sync"
)

type State string

const (
	StateReceived    State = "RECEIVED"
	StateAuthorized  State = "AUTHORIZED"
	StateQueued      State = "QUEUED"
	StateDispatching State = "DISPATCHING"
	StateRunning     State = "RUNNING"
	StateComplete    State = "COMPLETE"
	StateFailed      State = "FAILED"
	StateCanceled    State = "CANCELED"
)

var transitions = map[State]map[State]bool{
	StateReceived:    {StateAuthorized: true, StateFailed: true, StateCanceled: true},
	StateAuthorized:  {StateQueued: true, StateFailed: true, StateCanceled: true},
	StateQueued:      {StateDispatching: true, StateFailed: true, StateCanceled: true},
	StateDispatching: {StateRunning: true, StateFailed: true, StateCanceled: true},
	StateRunning:     {StateComplete: true, StateFailed: true, StateCanceled: true},
}

type stateMachine struct {
	mu    sync.Mutex
	state State
}

func newStateMachine() *stateMachine { return &stateMachine{state: StateReceived} }

func (machine *stateMachine) move(next State) error {
	machine.mu.Lock()
	defer machine.mu.Unlock()
	if !transitions[machine.state][next] {
		return fmt.Errorf("invalid execution transition %s -> %s", machine.state, next)
	}
	machine.state = next
	return nil
}

func (machine *stateMachine) current() State {
	machine.mu.Lock()
	defer machine.mu.Unlock()
	return machine.state
}
