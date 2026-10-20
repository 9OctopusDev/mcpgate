package legacy

import "github.com/mcpgate/mcpgate/internal/executor"

func cancellationKey(principal, session, request string) executor.CorrelationKey {
	return executor.CorrelationKey{Principal: principal, Session: session, Request: request}
}
