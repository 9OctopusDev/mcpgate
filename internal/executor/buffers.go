package executor

import (
	"errors"
	"sync/atomic"
)

var ErrBufferBudget = errors.New("shared buffer budget exceeded")

type bufferBudget struct {
	limit int64
	used  atomic.Int64
}

func newBufferBudget(limit int64) *bufferBudget { return &bufferBudget{limit: limit} }

func (budget *bufferBudget) reserve(bytes int64) (func(), error) {
	if bytes < 0 {
		bytes = 0
	}
	for {
		current := budget.used.Load()
		if bytes > budget.limit-current {
			return nil, ErrBufferBudget
		}
		if budget.used.CompareAndSwap(current, current+bytes) {
			var released atomic.Bool
			return func() {
				if released.CompareAndSwap(false, true) {
					budget.used.Add(-bytes)
				}
			}, nil
		}
	}
}
