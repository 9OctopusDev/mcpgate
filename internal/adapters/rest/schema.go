package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

const (
	maxDocumentBytes   = 2 << 20
	maxDocumentDepth   = 64
	maxDocumentNodes   = 10_000
	maxDocumentAliases = 64
)

type yamlLimitState struct {
	nodes   int
	aliases int
	err     error
}

type yamlLimitWalker struct {
	depth int
	state *yamlLimitState
}

func inspectYAML(document []byte) error {
	file, err := parser.ParseBytes(document, 0)
	if err != nil || len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return ErrUnsupportedOpenAPI
	}
	state := &yamlLimitState{}
	ast.Walk(&yamlLimitWalker{state: state}, file.Docs[0])
	return state.err
}

func (walker *yamlLimitWalker) Visit(node ast.Node) ast.Visitor {
	if node == nil || walker.state.err != nil {
		return nil
	}
	next := *walker
	next.depth++
	walker.state.nodes++
	if next.depth > maxDocumentDepth || walker.state.nodes > 100_000 {
		walker.state.err = ErrUnsupportedOpenAPI
		return nil
	}
	if _, ok := node.(*ast.AliasNode); ok {
		walker.state.aliases++
		if walker.state.aliases > maxDocumentAliases {
			walker.state.err = ErrUnsupportedOpenAPI
			return nil
		}
	}
	if _, ok := node.(*ast.TagNode); ok {
		walker.state.err = ErrUnsupportedOpenAPI
		return nil
	}
	return &next
}

func inspectDocument(value any, depth int, nodes *int) error {
	if depth > maxDocumentDepth {
		return fmt.Errorf("%w: maximum depth exceeded", ErrUnsupportedOpenAPI)
	}
	*nodes++
	if *nodes > maxDocumentNodes {
		return fmt.Errorf("%w: maximum schema nodes exceeded", ErrUnsupportedOpenAPI)
	}
	switch typed := value.(type) {
	case map[string]any:
		if reference, ok := typed["$ref"].(string); ok && reference != "" && reference[0] != '#' {
			return ErrExternalReference
		}
		for _, child := range typed {
			if err := inspectDocument(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := inspectDocument(child, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeSchema(schema map[string]any, version string) map[string]any {
	copy := cloneMap(schema)
	normalizeSchemaInPlace(copy, version)
	return copy
}

func normalizeSchemaInPlace(schema map[string]any, version string) {
	if reference, ok := schema["$ref"].(string); ok && strings.HasPrefix(reference, "#/components/schemas/") {
		schema["$ref"] = "#/$defs/" + strings.TrimPrefix(reference, "#/components/schemas/")
	}
	if version[:3] == "3.0" {
		if nullable, _ := schema["nullable"].(bool); nullable {
			typeValue := schema["type"]
			switch typed := typeValue.(type) {
			case string:
				schema["type"] = []any{typed, "null"}
			case []any:
				schema["type"] = append(typed, "null")
			default:
				delete(schema, "nullable")
				nonNull := cloneMap(schema)
				for key := range schema {
					delete(schema, key)
				}
				schema["anyOf"] = []any{nonNull, map[string]any{"type": "null"}}
			}
		}
		delete(schema, "nullable")
		if exclusive, ok := schema["exclusiveMinimum"].(bool); ok {
			if exclusive {
				schema["exclusiveMinimum"] = schema["minimum"]
			} else {
				delete(schema, "exclusiveMinimum")
			}
			if exclusive {
				delete(schema, "minimum")
			}
		}
		if exclusive, ok := schema["exclusiveMaximum"].(bool); ok {
			if exclusive {
				schema["exclusiveMaximum"] = schema["maximum"]
			} else {
				delete(schema, "exclusiveMaximum")
			}
			if exclusive {
				delete(schema, "maximum")
			}
		}
	}
	for _, value := range schema {
		switch child := value.(type) {
		case map[string]any:
			normalizeSchemaInPlace(child, version)
		case []any:
			for _, item := range child {
				if mapped, ok := item.(map[string]any); ok {
					normalizeSchemaInPlace(mapped, version)
				}
			}
		}
	}
}

func stripUnambiguousReadOnly(schema map[string]any) {
	typeName := schemaType(schema)
	properties, _ := schema["properties"].(map[string]any)
	if typeName != "object" || len(properties) == 0 {
		return
	}
	removed := make(map[string]struct{})
	for name, raw := range properties {
		property, ok := raw.(map[string]any)
		if ok {
			if readOnly, _ := property["readOnly"].(bool); readOnly {
				delete(properties, name)
				removed[name] = struct{}{}
				continue
			}
			stripUnambiguousReadOnly(property)
			if items, ok := property["items"].(map[string]any); ok {
				stripUnambiguousReadOnly(items)
			}
		}
	}
	if required, ok := schema["required"].([]any); ok && len(removed) > 0 {
		filtered := make([]any, 0, len(required))
		for _, raw := range required {
			name, _ := raw.(string)
			if _, drop := removed[name]; !drop {
				filtered = append(filtered, raw)
			}
		}
		schema["required"] = filtered
	}
}

func buildInputSchema(pathParameters, queryParameters []ParameterPlan, body *BodyPlan, definitions map[string]any) json.RawMessage {
	properties := make(map[string]any)
	requiredGroups := make([]string, 0, 3)
	if len(pathParameters) > 0 {
		group, required := parameterGroup(pathParameters)
		properties["path"] = group
		if required {
			requiredGroups = append(requiredGroups, "path")
		}
	}
	if len(queryParameters) > 0 {
		group, required := parameterGroup(queryParameters)
		properties["query"] = group
		if required {
			requiredGroups = append(requiredGroups, "query")
		}
	}
	if body != nil {
		var bodySchema any
		_ = json.Unmarshal(body.Schema, &bodySchema)
		properties["body"] = bodySchema
		if body.Required {
			requiredGroups = append(requiredGroups, "body")
		}
	}
	root := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(definitions) > 0 {
		root["$defs"] = definitions
	}
	if len(requiredGroups) > 0 {
		sort.Strings(requiredGroups)
		root["required"] = requiredGroups
	}
	encoded, _ := json.Marshal(root)
	return encoded
}

func compileDefinitions(root map[string]any, version string, forRequest bool) map[string]any {
	components, _ := root["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	definitions := make(map[string]any, len(schemas))
	for name, raw := range schemas {
		if schema, ok := raw.(map[string]any); ok {
			normalized := normalizeSchema(schema, version)
			if forRequest {
				stripUnambiguousReadOnly(normalized)
			}
			definitions[name] = normalized
		}
	}
	return definitions
}

func attachDefinitions(schema map[string]any, definitions map[string]any) map[string]any {
	if len(definitions) > 0 {
		schema["$defs"] = cloneMap(definitions)
	}
	return schema
}

func parameterGroup(parameters []ParameterPlan) (map[string]any, bool) {
	properties := make(map[string]any, len(parameters))
	required := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		var schema any
		_ = json.Unmarshal(parameter.Schema, &schema)
		properties[parameter.Name] = schema
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}
	group := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		sort.Strings(required)
		group["required"] = required
	}
	return group, len(required) > 0
}

func cloneMap(source map[string]any) map[string]any {
	encoded, _ := json.Marshal(source)
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	_ = decoder.Decode(&result)
	return result
}
