package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
	"github.com/9OctopusDev/mcpgate/internal/adapters/rest"
	"github.com/9OctopusDev/mcpgate/internal/config"
)

type validationReport struct {
	Valid               bool                `json:"valid"`
	Online              bool                `json:"online"`
	Ready               bool                `json:"ready"`
	Diagnostics         []config.Diagnostic `json:"diagnostics"`
	UnresolvedUpstreams []string            `json:"unresolved_upstreams,omitempty"`
}

func loadForCLI(ctx context.Context, path string) (*config.Document, []config.Diagnostic) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, []config.Diagnostic{{Code: "SOURCE_PATH_INVALID", File: path, Line: 1, Column: 1, ConfigPath: "/", Message: "configuration path is invalid"}}
	}
	root := filepath.Dir(absolute)
	return config.Load(ctx, absolute, config.SourceLimits{Roots: []string{root}, MaxBytes: 2 << 20})
}

func validateDocument(ctx context.Context, path string, online bool) (*config.Document, validationReport) {
	document, diagnostics := loadForCLI(ctx, path)
	report := validationReport{Valid: len(diagnostics) == 0, Online: online, Ready: false, Diagnostics: diagnostics}
	if document == nil {
		return nil, report
	}
	base := filepath.Dir(document.Source.Resolved)
	sourceRoot := filepath.Dir(base)
	for upstreamID, upstream := range document.Upstreams {
		if err := ctx.Err(); err != nil {
			report.Valid = false
			report.Diagnostics = append(report.Diagnostics, config.Diagnostic{Code: "VALIDATE_CANCELLED", File: path, Line: 1, Column: 1, ConfigPath: "/", Message: "validation cancelled"})
			break
		}
		switch upstream.Kind {
		case "rest":
			content, _, err := config.ReadLocal(base, upstream.OpenAPI.File, []string{base, sourceRoot}, document.Gateway.Limits.SchemaBytes)
			if err != nil {
				report.Valid = false
				report.Diagnostics = append(report.Diagnostics, config.Diagnostic{Code: "OPENAPI_READ_FAILED", File: upstream.OpenAPI.File, Line: 1, Column: 1, ConfigPath: "/upstreams/" + upstreamID + "/openapi/file", Message: "local OpenAPI document could not be read"})
				continue
			}
			for _, server := range document.Servers {
				for _, export := range server.Exports {
					if export.Upstream == upstreamID {
						bindings := map[string]bool{}
						if upstream.Auth.Kind != "" {
							var parsed struct {
								Components struct {
									SecuritySchemes map[string]any `yaml:"securitySchemes"`
								} `yaml:"components"`
							}
							if yaml.Unmarshal(content, &parsed) == nil {
								for name := range parsed.Components.SecuritySchemes {
									bindings[name] = true
								}
							}
						}
						if _, err := rest.Compile(content, rest.CompileOptions{OperationID: export.Native, EndpointBase: upstream.Endpoints[0].URL, SecurityBindings: bindings, MaxDocumentBytes: int(document.Gateway.Limits.SchemaBytes)}); err != nil {
							report.Valid = false
							report.Diagnostics = append(report.Diagnostics, config.Diagnostic{Code: "OPENAPI_COMPILE_FAILED", File: upstream.OpenAPI.File, Line: 1, Column: 1, ConfigPath: "/upstreams/" + upstreamID + "/openapi", OperationID: export.Native, Message: "OpenAPI operation could not be compiled"})
						}
					}
				}
			}
		default:
			report.UnresolvedUpstreams = append(report.UnresolvedUpstreams, upstreamID)
		}
	}
	sort.Strings(report.UnresolvedUpstreams)
	report.Ready = report.Valid && online && len(report.UnresolvedUpstreams) == 0
	return document, report
}

func runValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration file")
	online := flags.Bool("online", false, "perform safe online discovery checks")
	if flags.Parse(args) != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "validate requires --config")
		return 2
	}
	_, report := validateDocument(ctx, *path, *online)
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return 1
	}
	if !report.Valid {
		return 1
	}
	if *online && len(report.UnresolvedUpstreams) > 0 {
		return 1
	}
	return 0
}

func runServeCommand(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "configuration file")
	if flags.Parse(args) != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(stderr, "serve requires --config")
		return 2
	}
	runtime, err := newConfiguredRuntime(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := Serve(ctx, runtime.coordinator); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
