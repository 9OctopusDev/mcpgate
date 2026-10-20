package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	gatewayruntime "github.com/mcpgate/mcpgate/internal/runtime"
)

func Serve(ctx context.Context, coordinator *gatewayruntime.Coordinator) error {
	if coordinator == nil {
		return errors.New("runtime coordinator is required")
	}
	if err := coordinator.Start(ctx); err != nil {
		return err
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	for {
		select {
		case <-ctx.Done():
			return coordinator.Shutdown(context.Background())
		case received := <-signals:
			switch received {
			case syscall.SIGHUP:
				_ = coordinator.Reload(ctx)
			default:
				return coordinator.Shutdown(context.Background())
			}
		}
	}
}
