// Package app wires the mcpgate command-line interface to application
// services. Subcommands own their flag sets so parsing never mutates global
// process state.
package app

import (
	"context"
	"fmt"
	"io"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: mcpgate <validate|inspect|serve|healthcheck|version>")
		return 2
	}

	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "validate":
		return runValidate(ctx, args[1:], stdout, stderr)
	case "inspect":
		return runInspect(ctx, args[1:], stdout, stderr)
	case "serve":
		return runServeCommand(ctx, args[1:], stdout, stderr)
	case "healthcheck":
		return runHealthcheck(ctx, args[1:], stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}
