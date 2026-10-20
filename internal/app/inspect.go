package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
)

type inspectedExport struct {
	Server    string   `json:"server"`
	Name      string   `json:"name"`
	Upstream  string   `json:"upstream"`
	Kind      string   `json:"kind"`
	Native    string   `json:"native"`
	Policy    string   `json:"policy"`
	Behavior  string   `json:"behavior"`
	Protocols []string `json:"protocols"`
	State     string   `json:"state"`
}
type inspection struct {
	ConfigVersion       string            `json:"config_version"`
	Exports             []inspectedExport `json:"exports"`
	UnresolvedUpstreams []string          `json:"unresolved_upstreams,omitempty"`
	Diagnostics         any               `json:"diagnostics"`
}

func runInspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration file")
	format := flags.String("format", "json", "output format")
	if flags.Parse(args) != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "inspect requires --config")
		return 2
	}
	if *format != "json" {
		fmt.Fprintln(stderr, "inspect --format must be json")
		return 2
	}
	document, report := validateDocument(ctx, *path, false)
	if document == nil {
		_ = json.NewEncoder(stdout).Encode(report)
		return 1
	}
	result := inspection{ConfigVersion: document.APIVersion, UnresolvedUpstreams: report.UnresolvedUpstreams, Diagnostics: report.Diagnostics}
	serverIDs := make([]string, 0, len(document.Servers))
	for id := range document.Servers {
		serverIDs = append(serverIDs, id)
	}
	sort.Strings(serverIDs)
	for _, serverID := range serverIDs {
		server := document.Servers[serverID]
		for _, export := range server.Exports {
			upstream := document.Upstreams[export.Upstream]
			state := "available"
			if upstream.Kind != "rest" {
				state = "unresolved"
			}
			result.Exports = append(result.Exports, inspectedExport{Server: serverID, Name: export.Name, Upstream: export.Upstream, Kind: upstream.Kind, Native: export.Native, Policy: export.Policy, Behavior: export.Behavior, Protocols: append([]string(nil), server.Protocols...), State: state})
		}
	}
	sort.Slice(result.Exports, func(i, j int) bool {
		if result.Exports[i].Server != result.Exports[j].Server {
			return result.Exports[i].Server < result.Exports[j].Server
		}
		return result.Exports[i].Name < result.Exports[j].Name
	})
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	if !report.Valid {
		return 1
	}
	return 0
}
