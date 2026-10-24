package legacy

import (
	"encoding/json"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

func CompatibleTools(tools []contracts.ToolContract) []contracts.ToolContract {
	result := make([]contracts.ToolContract, 0, len(tools))
	for _, tool := range tools {
		if len(tool.OutputSchema) > 0 {
			var schema struct {
				Type any `json:"type"`
			}
			if json.Unmarshal(tool.OutputSchema, &schema) != nil || !objectType(schema.Type) {
				continue
			}
		}
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		tool.OutputSchema = append(json.RawMessage(nil), tool.OutputSchema...)
		result = append(result, tool)
	}
	return result
}

func objectType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "object"
	case []any:
		for _, entry := range typed {
			if entry == "object" {
				return true
			}
		}
		return false
	default:
		return false
	}
}
