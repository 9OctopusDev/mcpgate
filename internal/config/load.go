package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/9OctopusDev/mcpgate/schemas"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	defaultConfigBytes = 2 << 20
	defaultMaxDepth    = 64
	defaultMaxAliases  = 64
	defaultMaxNodes    = 100_000
)

type SourceLimits struct {
	Roots      []string
	MaxBytes   int64
	MaxDepth   int
	MaxAliases int
	MaxNodes   int
}

func (l SourceLimits) withDefaults() SourceLimits {
	if l.MaxBytes == 0 {
		l.MaxBytes = defaultConfigBytes
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = defaultMaxDepth
	}
	if l.MaxAliases == 0 {
		l.MaxAliases = defaultMaxAliases
	}
	if l.MaxNodes == 0 {
		l.MaxNodes = defaultMaxNodes
	}
	return l
}

func Load(ctx context.Context, path string, limits SourceLimits) (*Document, []Diagnostic) {
	limits = limits.withDefaults()
	if err := ctx.Err(); err != nil {
		return nil, []Diagnostic{diagnostic("LOAD_CANCELLED", path, 1, 1, "/", "configuration load cancelled")}
	}
	content, source, err := ReadLocal(filepath.Dir(path), path, limits.Roots, limits.MaxBytes)
	if err != nil {
		code := "SOURCE_READ_FAILED"
		var sourceErr *SourceError
		if errors.As(err, &sourceErr) {
			code = sourceErr.Code
		}
		return nil, []Diagnostic{diagnostic(code, path, 1, 1, "/", safeSourceMessage(code))}
	}
	if !utf8.Valid(content) {
		return nil, []Diagnostic{diagnostic("YAML_INVALID_UTF8", path, 1, 1, "/", "configuration must be UTF-8")}
	}

	file, err := parser.ParseBytes(content, 0)
	if err != nil {
		return nil, []Diagnostic{yamlErrorDiagnostic(path, err)}
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return nil, []Diagnostic{diagnostic("YAML_DOCUMENT_COUNT", path, 1, 1, "/", "configuration must contain exactly one YAML document")}
	}
	walker := &limitWalker{
		maxDepth: limits.MaxDepth, maxAliases: limits.MaxAliases, maxNodes: limits.MaxNodes,
		state: &walkState{},
	}
	ast.Walk(walker, file.Docs[0])
	if walker.state.err != nil {
		return nil, []Diagnostic{diagnostic(walker.state.err.code, path, walker.state.err.line, walker.state.err.column, "/", walker.state.err.message)}
	}

	jsonBytes, err := yaml.YAMLToJSON(content)
	if err != nil {
		return nil, []Diagnostic{yamlErrorDiagnostic(path, err)}
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, []Diagnostic{diagnostic("CONFIG_JSON_INVALID", path, 1, 1, "/", "configuration could not be normalized")}
	}
	schema, err := compiledGatewaySchema()
	if err != nil {
		return nil, []Diagnostic{diagnostic("CONFIG_SCHEMA_INTERNAL", path, 1, 1, "/", "embedded configuration schema is unavailable")}
	}
	if err := schema.Validate(instance); err != nil {
		configPath := "/"
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) && len(validationErr.InstanceLocation) > 0 {
			configPath += strings.Join(validationErr.InstanceLocation, "/")
		}
		return nil, []Diagnostic{diagnostic("CONFIG_SCHEMA_INVALID", path, 1, 1, configPath, "configuration does not satisfy mcpgate/v1")}
	}

	var document Document
	if err := yaml.UnmarshalWithOptions(content, &document, yaml.Strict()); err != nil {
		return nil, []Diagnostic{yamlDecodeDiagnostic(path, err)}
	}
	applyDefaults(&document)
	document.Source = source
	return &document, nil
}

type walkError struct {
	code, message string
	line, column  int
}

type limitWalker struct {
	depth                          int
	maxDepth, maxNodes, maxAliases int
	state                          *walkState
}

type walkState struct {
	nodes, aliases int
	err            *walkError
}

func (w *limitWalker) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if w.state.err != nil {
		return nil
	}
	child := *w
	child.depth++
	w.state.nodes++
	line, column := nodePosition(node)
	if child.depth > w.maxDepth {
		w.state.err = &walkError{code: "YAML_DEPTH_LIMIT", message: "YAML nesting exceeds the configured limit", line: line, column: column}
		return nil
	}
	if w.state.nodes > w.maxNodes {
		w.state.err = &walkError{code: "YAML_NODE_LIMIT", message: "YAML node count exceeds the configured limit", line: line, column: column}
		return nil
	}
	switch typed := node.(type) {
	case *ast.AliasNode:
		w.state.aliases++
		if w.state.aliases > w.maxAliases {
			w.state.err = &walkError{code: "YAML_ALIAS_LIMIT", message: "YAML alias count exceeds the configured limit", line: line, column: column}
			return nil
		}
	case *ast.TagNode:
		if !isCoreTag(typed.Start.Value) {
			w.state.err = &walkError{code: "YAML_CUSTOM_TAG", message: "custom YAML tags are not supported", line: line, column: column}
			return nil
		}
	}
	return &child
}

func isCoreTag(value string) bool {
	switch value {
	case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!map", "!!seq", "!!timestamp":
		return true
	default:
		return false
	}
}

func nodePosition(node ast.Node) (int, int) {
	if node == nil || node.GetToken() == nil || node.GetToken().Position == nil {
		return 1, 1
	}
	return max(1, node.GetToken().Position.Line), max(1, node.GetToken().Position.Column)
}

func yamlErrorDiagnostic(path string, err error) Diagnostic {
	code := "YAML_SYNTAX_INVALID"
	var duplicate *yaml.DuplicateKeyError
	if errors.As(err, &duplicate) || strings.Contains(err.Error(), "already defined") {
		code = "YAML_DUPLICATE_KEY"
	}
	line, column := errorPosition(err)
	return diagnostic(code, path, line, column, "/", safeYAMLMessage(code))
}

func yamlDecodeDiagnostic(path string, err error) Diagnostic {
	code := "CONFIG_DECODE_INVALID"
	var unknown *yaml.UnknownFieldError
	if errors.As(err, &unknown) {
		code = "CONFIG_UNKNOWN_FIELD"
	}
	line, column := errorPosition(err)
	return diagnostic(code, path, line, column, "/", safeYAMLMessage(code))
}

type positionedError interface {
	GetToken() *token.Token
}

func errorPosition(err error) (int, int) {
	var positioned positionedError
	if errors.As(err, &positioned) && positioned.GetToken() != nil && positioned.GetToken().Position != nil {
		return max(1, positioned.GetToken().Position.Line), max(1, positioned.GetToken().Position.Column)
	}
	return 1, 1
}

func diagnostic(code, file string, line, column int, configPath, message string) Diagnostic {
	return Diagnostic{Code: code, File: file, Line: max(1, line), Column: max(1, column), ConfigPath: configPath, Message: message}
}

func safeSourceMessage(code string) string {
	switch code {
	case "SOURCE_TOO_LARGE":
		return "configuration exceeds the configured byte limit"
	case "SOURCE_ROOT_ESCAPE":
		return "configuration path escapes an allowed source root"
	default:
		return "configuration source could not be read"
	}
}

func safeYAMLMessage(code string) string {
	switch code {
	case "YAML_DUPLICATE_KEY":
		return "duplicate YAML mapping key"
	case "CONFIG_UNKNOWN_FIELD":
		return "unknown configuration field"
	default:
		return "configuration YAML is invalid"
	}
}

var (
	schemaOnce sync.Once
	schemaV1   *jsonschema.Schema
	schemaErr  error
)

func compiledGatewaySchema() (*jsonschema.Schema, error) {
	schemaOnce.Do(func() {
		resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemas.GatewayConfigV1))
		if err != nil {
			schemaErr = fmt.Errorf("decode embedded schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		const schemaURL = "https://mcpgate.dev/schemas/gateway-config.v1.schema.json"
		if err := compiler.AddResource(schemaURL, resource); err != nil {
			schemaErr = fmt.Errorf("add embedded schema: %w", err)
			return
		}
		schemaV1, schemaErr = compiler.Compile(schemaURL)
	})
	return schemaV1, schemaErr
}
