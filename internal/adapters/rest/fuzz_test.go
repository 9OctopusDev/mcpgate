package rest

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzPathAndQuerySerialization(f *testing.F) {
	f.Add([]byte(`{"value":"safe","query":"x"}`))
	f.Add([]byte(`{"value":"../escape","query":["a","b"]}`))
	plan := RESTPlan{PathSegments: []PathSegment{{Kind: LiteralSegment, Value: "/items/"}, {Kind: ParameterSegment, Value: "value"}}, QueryParameters: []ParameterPlan{{Name: "query", Location: "query", Array: true, Explode: true, Nullable: false}}}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1<<16 {
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(input))
		decoder.UseNumber()
		var arguments map[string]any
		if decoder.Decode(&arguments) != nil {
			return
		}
		path, _ := plan.BuildPath(arguments)
		if path == "/items/../escape" {
			t.Fatal("path traversal survived serialization")
		}
		query := map[string]any{}
		if value, ok := arguments["query"]; ok {
			query["query"] = value
		}
		_, _ = plan.BuildQuery(query)
	})
}
