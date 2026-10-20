package rest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func (plan *RESTPlan) BuildPath(arguments map[string]any) (string, error) {
	var builder strings.Builder
	for _, segment := range plan.PathSegments {
		if segment.Kind == LiteralSegment {
			builder.WriteString(segment.Value)
			continue
		}
		value, exists := arguments[segment.Value]
		if !exists || value == nil {
			return "", fmt.Errorf("%w: missing path parameter %s", ErrInvalidParameter, segment.Value)
		}
		text, ok := scalarText(value)
		if !ok || unsafePathValue(text) {
			return "", fmt.Errorf("%w: unsafe path parameter %s", ErrInvalidParameter, segment.Value)
		}
		builder.WriteString(url.PathEscape(text))
	}
	return builder.String(), nil
}

func (plan *RESTPlan) BuildQuery(arguments map[string]any) (string, error) {
	values := make(url.Values)
	known := make(map[string]ParameterPlan, len(plan.QueryParameters))
	for _, parameter := range plan.QueryParameters {
		known[parameter.Name] = parameter
	}
	for name := range arguments {
		if _, ok := known[name]; !ok {
			return "", fmt.Errorf("%w: unknown query parameter %s", ErrInvalidParameter, name)
		}
	}
	for _, parameter := range plan.QueryParameters {
		value, exists := arguments[parameter.Name]
		if !exists {
			if parameter.Required {
				return "", fmt.Errorf("%w: missing query parameter %s", ErrInvalidParameter, parameter.Name)
			}
			continue
		}
		if value == nil {
			if !parameter.Nullable {
				return "", fmt.Errorf("%w: null query parameter %s", ErrInvalidParameter, parameter.Name)
			}
			values.Add(parameter.Name, "null")
			continue
		}
		if parameter.Array {
			items, ok := value.([]any)
			if !ok {
				return "", fmt.Errorf("%w: query parameter %s must be an array", ErrInvalidParameter, parameter.Name)
			}
			encoded := make([]string, 0, len(items))
			for _, item := range items {
				text, ok := scalarText(item)
				if !ok {
					return "", fmt.Errorf("%w: invalid array value", ErrInvalidParameter)
				}
				encoded = append(encoded, text)
			}
			if parameter.Explode {
				for _, item := range encoded {
					values.Add(parameter.Name, item)
				}
			} else {
				values.Add(parameter.Name, strings.Join(encoded, ","))
			}
			continue
		}
		text, ok := scalarText(value)
		if !ok {
			return "", fmt.Errorf("%w: query parameter %s must be scalar", ErrInvalidParameter, parameter.Name)
		}
		values.Add(parameter.Name, text)
	}
	return values.Encode(), nil
}

func scalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case json.Number:
		return typed.String(), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	default:
		return "", false
	}
}

func unsafePathValue(value string) bool {
	lower := strings.ToLower(value)
	return value == "." || value == ".." || strings.ContainsAny(value, `/\`) || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e")
}
