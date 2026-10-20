package faults_test

import (
	"context"
	"testing"
	"time"

	gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"
)

func TestShutdownUnderTrafficRejectsNewLeases(t *testing.T) {
	store := gatewayruntime.NewStore(gatewayruntime.NewGeneration(gatewayruntime.GenerationSpec{Revision: "active", DrainTimeout: time.Second}))
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := store.Shutdown(ctx); err == nil {
		t.Fatal("shutdown with held lease should hit deadline")
	}
	if _, err := store.Acquire(); err == nil {
		t.Fatal("new lease admitted during shutdown")
	}
	lease.Release()
}
