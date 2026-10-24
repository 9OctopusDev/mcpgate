package executor

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

type retryable interface{ Retryable() bool }

func mayRetry(route contracts.Route, attempt int, err error) bool {
	if route.Behavior != contracts.BehaviorReadOnly || attempt >= route.MaxAttempts || err == nil {
		return false
	}
	retry, ok := err.(retryable)
	return ok && retry.Retryable()
}

func waitRetry(ctx context.Context, attempt int) error {
	maximum := 25 * time.Millisecond * time.Duration(1<<min(attempt, 6))
	delay := time.Duration(rand.Int64N(int64(maximum) + 1))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
