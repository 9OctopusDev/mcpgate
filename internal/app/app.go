// Package app wires the mcpgate command-line interface to application
// services. Subcommands own their flag sets so parsing never mutates global
// process state.
package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"runtime"
)

var (
	Version        = "dev"
	Commit         = "unknown"
	BuildGoVersion = runtime.Version()
)

type versionInfo struct {
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	GoVersion     string            `json:"go_version"`
	ConfigVersion string            `json:"config_version"`
	MCPVersions   []string          `json:"mcp_versions"`
	Dependencies  map[string]string `json:"dependencies"`
}

func Run(_ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: mcpgate <validate|inspect|serve|version>")
		return 2
	}

	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "validate", "inspect", "serve":
		fmt.Fprintf(stderr, "%s is not implemented\n", args[0])
		return 2
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "emit machine-readable build information")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "version accepts no positional arguments")
		return 2
	}

	info := versionInfo{
		Version:       Version,
		Commit:        Commit,
		GoVersion:     BuildGoVersion,
		ConfigVersion: "mcpgate/v1",
		MCPVersions:   []string{"2026-07-28", "2025-11-25"},
		Dependencies: map[string]string{
			"github.com/modelcontextprotocol/go-sdk":   "v1.8.0",
			"github.com/pb33f/libopenapi":              "v0.41.6",
			"github.com/santhosh-tekuri/jsonschema/v6": "v6.0.3",
			"github.com/goccy/go-yaml":                 "v1.19.2",
			"github.com/lestrrat-go/jwx/v4":            "v4.1.0",
			"github.com/prometheus/client_golang":      "v1.24.1",
			"go.opentelemetry.io/otel":                 "v1.47.0",
		},
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(info); err != nil {
			fmt.Fprintln(stderr, "encode version information")
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "mcpgate %s (%s) %s\n", info.Version, info.Commit, info.GoVersion)
	return 0
}
