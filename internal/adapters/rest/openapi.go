package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
)

type selectedOperation struct {
	path           string
	method         string
	operation      map[string]any
	pathParameters []any
}

func Compile(documentBytes []byte, options CompileOptions) (*RESTPlan, error) {
	limit := options.MaxDocumentBytes
	if limit <= 0 {
		limit = maxDocumentBytes
	}
	if len(documentBytes) > limit {
		return nil, ErrDocumentTooLarge
	}
	if options.OperationID == "" {
		return nil, ErrOperationNotFound
	}
	if err := inspectYAML(documentBytes); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(options.EndpointBase)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("invalid operator endpoint")
	}

	jsonBytes, err := yaml.YAMLToJSON(documentBytes)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI: %w", ErrUnsupportedOpenAPI)
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse OpenAPI: %w", ErrUnsupportedOpenAPI)
	}
	nodes := 0
	if err := inspectDocument(root, 0, &nodes); err != nil {
		return nil, err
	}
	configuration := datamodel.NewDocumentConfiguration()
	configuration.AllowFileReferences = false
	configuration.AllowRemoteReferences = false
	configuration.SkipExternalRefResolution = true
	document, err := libopenapi.NewDocumentWithConfiguration(documentBytes, configuration)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI: %w", ErrUnsupportedOpenAPI)
	}
	version := document.GetVersion()
	if !strings.HasPrefix(version, "3.0.") && !strings.HasPrefix(version, "3.1.") {
		return nil, ErrUnsupportedOpenAPI
	}
	if dialect, _ := root["jsonSchemaDialect"].(string); dialect != "" && dialect != "https://json-schema.org/draft/2020-12/schema" && dialect != "https://spec.openapis.org/oas/3.1/dialect/base" {
		return nil, ErrUnsupportedOpenAPI
	}
	if webhooks, ok := root["webhooks"].(map[string]any); ok && len(webhooks) > 0 {
		return nil, ErrUnsupportedOpenAPI
	}
	selected, err := findOperation(root, options.OperationID)
	if err != nil {
		return nil, err
	}
	if callbacks, ok := selected.operation["callbacks"].(map[string]any); ok && len(callbacks) > 0 {
		return nil, ErrUnsupportedOpenAPI
	}

	requestDefinitions := compileDefinitions(root, version, true)
	responseDefinitions := compileDefinitions(root, version, false)
	pathParameters, queryParameters, err := compileParameters(root, append(selected.pathParameters, arrayValue(selected.operation["parameters"])...), version)
	if err != nil {
		return nil, err
	}
	segments, err := compilePath(selected.path, pathParameters)
	if err != nil {
		return nil, err
	}
	body, err := compileBody(root, selected.operation, version, requestDefinitions)
	if err != nil {
		return nil, err
	}
	security, err := selectSecurity(root, selected.operation, options.SecurityBindings)
	if err != nil {
		return nil, err
	}
	responses, err := compileResponses(root, selected.operation, version, responseDefinitions)
	if err != nil {
		return nil, err
	}
	plan := &RESTPlan{
		OperationID: options.OperationID, Method: strings.ToUpper(selected.method), EndpointBase: strings.TrimSuffix(options.EndpointBase, "/"),
		PathTemplate: selected.path, PathSegments: segments, PathParameters: pathParameters, QueryParameters: queryParameters,
		Body: body, ResponseSchemas: responses, SecuritySchemes: security,
	}
	plan.InputSchema = buildInputSchema(pathParameters, queryParameters, body, requestDefinitions)
	plan.ReadOnly = plan.Method == "GET" || plan.Method == "HEAD"
	return plan, nil
}

func findOperation(root map[string]any, wanted string) (selectedOperation, error) {
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return selectedOperation{}, ErrOperationNotFound
	}
	pathNames := sortedMapKeys(paths)
	found := make(map[string]selectedOperation)
	for _, path := range pathNames {
		pathItem, ok := paths[path].(map[string]any)
		if !ok {
			continue
		}
		pathParameters := arrayValue(pathItem["parameters"])
		for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
			operation, ok := pathItem[method].(map[string]any)
			if !ok {
				continue
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				continue
			}
			if _, duplicate := found[operationID]; duplicate {
				return selectedOperation{}, ErrDuplicateOperationID
			}
			found[operationID] = selectedOperation{path: path, method: method, operation: operation, pathParameters: pathParameters}
		}
	}
	selected, ok := found[wanted]
	if !ok {
		return selectedOperation{}, ErrOperationNotFound
	}
	return selected, nil
}

func compileParameters(root map[string]any, raw []any, version string) ([]ParameterPlan, []ParameterPlan, error) {
	var pathParameters, queryParameters []ParameterPlan
	seen := make(map[string]struct{})
	for _, item := range raw {
		parameter, ok := item.(map[string]any)
		if !ok {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		parameter, err := resolveObject(root, parameter, 0)
		if err != nil || parameter["content"] != nil {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		if allowReserved, _ := parameter["allowReserved"].(bool); allowReserved {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		name, _ := parameter["name"].(string)
		location, _ := parameter["in"].(string)
		if name == "" || (location != "path" && location != "query") {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		identity := location + "\x00" + name
		if _, duplicate := seen[identity]; duplicate {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		seen[identity] = struct{}{}
		schema, ok := parameter["schema"].(map[string]any)
		if !ok {
			return nil, nil, ErrUnsupportedOpenAPI
		}
		normalized := normalizeSchema(schema, version)
		array := schemaType(normalized) == "array"
		nullable := schemaNullable(normalized)
		required, _ := parameter["required"].(bool)
		style, _ := parameter["style"].(string)
		explode, explodeSet := parameter["explode"].(bool)
		if location == "path" {
			if !required || (style != "" && style != "simple") || array || !scalarSchema(normalized) {
				return nil, nil, ErrUnsupportedOpenAPI
			}
			explode = false
		} else {
			if style != "" && style != "form" {
				return nil, nil, ErrUnsupportedOpenAPI
			}
			if !explodeSet {
				explode = true
			}
			if array {
				items, ok := normalized["items"].(map[string]any)
				if !ok || !scalarSchema(items) {
					return nil, nil, ErrUnsupportedOpenAPI
				}
			} else if !scalarSchema(normalized) {
				return nil, nil, ErrUnsupportedOpenAPI
			}
		}
		encoded, _ := json.Marshal(normalized)
		plan := ParameterPlan{Name: name, Location: location, Required: required, Array: array, Explode: explode, Nullable: nullable, Schema: encoded}
		if location == "path" {
			pathParameters = append(pathParameters, plan)
		} else {
			queryParameters = append(queryParameters, plan)
		}
	}
	sort.Slice(pathParameters, func(i, j int) bool { return pathParameters[i].Name < pathParameters[j].Name })
	sort.Slice(queryParameters, func(i, j int) bool { return queryParameters[i].Name < queryParameters[j].Name })
	return pathParameters, queryParameters, nil
}

func compilePath(path string, parameters []ParameterPlan) ([]PathSegment, error) {
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." || strings.Contains(strings.ToLower(part), "%2e") || strings.Contains(strings.ToLower(part), "%2f") || strings.Contains(strings.ToLower(part), "%5c") {
			return nil, ErrUnsupportedOpenAPI
		}
	}
	known := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		known[parameter.Name] = struct{}{}
	}
	var segments []PathSegment
	remaining := path
	for len(remaining) > 0 {
		open := strings.IndexByte(remaining, '{')
		if open < 0 {
			segments = append(segments, PathSegment{Kind: LiteralSegment, Value: remaining})
			break
		}
		if open > 0 {
			segments = append(segments, PathSegment{Kind: LiteralSegment, Value: remaining[:open]})
		}
		closeOffset := strings.IndexByte(remaining[open:], '}')
		if closeOffset < 0 {
			return nil, ErrUnsupportedOpenAPI
		}
		closeIndex := open + closeOffset
		name := remaining[open+1 : closeIndex]
		if _, ok := known[name]; !ok || name == "" {
			return nil, ErrUnsupportedOpenAPI
		}
		delete(known, name)
		segments = append(segments, PathSegment{Kind: ParameterSegment, Value: name})
		remaining = remaining[closeIndex+1:]
	}
	if len(known) != 0 {
		return nil, ErrUnsupportedOpenAPI
	}
	return segments, nil
}

func compileBody(root, operation map[string]any, version string, definitions map[string]any) (*BodyPlan, error) {
	raw, present := operation["requestBody"]
	if !present {
		return nil, nil
	}
	body, ok := raw.(map[string]any)
	if !ok {
		return nil, ErrUnsupportedOpenAPI
	}
	body, err := resolveObject(root, body, 0)
	if err != nil {
		return nil, ErrUnsupportedOpenAPI
	}
	content, ok := body["content"].(map[string]any)
	if !ok || len(content) != 1 {
		return nil, ErrUnsupportedOpenAPI
	}
	media, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil, ErrUnsupportedOpenAPI
	}
	schema, ok := media["schema"].(map[string]any)
	if !ok {
		return nil, ErrUnsupportedOpenAPI
	}
	normalized := normalizeSchema(schema, version)
	stripUnambiguousReadOnly(normalized)
	if containsBinarySchema(normalized) {
		return nil, ErrUnsupportedOpenAPI
	}
	encoded, _ := json.Marshal(attachDefinitions(normalized, definitions))
	required, _ := body["required"].(bool)
	return &BodyPlan{Required: required, Schema: encoded}, nil
}

func compileResponses(root, operation map[string]any, version string, definitions map[string]any) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	responses, _ := operation["responses"].(map[string]any)
	for status, raw := range responses {
		response, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		response, err := resolveObject(root, response, 0)
		if err != nil {
			return nil, ErrUnsupportedOpenAPI
		}
		content, _ := response["content"].(map[string]any)
		media, _ := content["application/json"].(map[string]any)
		schema, _ := media["schema"].(map[string]any)
		if schema != nil {
			encoded, _ := json.Marshal(attachDefinitions(normalizeSchema(schema, version), definitions))
			result[status] = encoded
		}
	}
	return result, nil
}

func scalarSchema(schema map[string]any) bool {
	typeName := schemaType(schema)
	return typeName == "string" || typeName == "integer" || typeName == "number" || typeName == "boolean"
}

func schemaType(schema map[string]any) string {
	if value, ok := schema["type"].(string); ok {
		return value
	}
	if values, ok := schema["type"].([]any); ok {
		for _, raw := range values {
			if value, ok := raw.(string); ok && value != "null" {
				return value
			}
		}
	}
	return ""
}

func schemaNullable(schema map[string]any) bool {
	values, _ := schema["type"].([]any)
	for _, raw := range values {
		if raw == "null" {
			return true
		}
	}
	return false
}

func containsBinarySchema(schema map[string]any) bool {
	if format, _ := schema["format"].(string); format == "binary" || format == "byte" {
		return true
	}
	for _, value := range schema {
		switch typed := value.(type) {
		case map[string]any:
			if containsBinarySchema(typed) {
				return true
			}
		case []any:
			for _, item := range typed {
				if child, ok := item.(map[string]any); ok && containsBinarySchema(child) {
					return true
				}
			}
		}
	}
	return false
}

func arrayValue(value any) []any { values, _ := value.([]any); return values }

func resolveObject(root, object map[string]any, depth int) (map[string]any, error) {
	if depth > 16 {
		return nil, ErrUnsupportedOpenAPI
	}
	reference, _ := object["$ref"].(string)
	if reference == "" {
		return object, nil
	}
	if !strings.HasPrefix(reference, "#/") {
		return nil, ErrExternalReference
	}
	var current any = root
	for _, encoded := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		name := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		mapped, ok := current.(map[string]any)
		if !ok {
			return nil, ErrUnsupportedOpenAPI
		}
		current, ok = mapped[name]
		if !ok {
			return nil, ErrUnsupportedOpenAPI
		}
	}
	resolved, ok := current.(map[string]any)
	if !ok {
		return nil, ErrUnsupportedOpenAPI
	}
	return resolveObject(root, resolved, depth+1)
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
