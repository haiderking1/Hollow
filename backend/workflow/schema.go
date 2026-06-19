package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func stripJSONFences(text string) string {
	s := strings.TrimSpace(text)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i:]
		if strings.HasPrefix(s, "```") {
			if nl := strings.IndexByte(s, '\n'); nl >= 0 {
				s = s[nl+1:]
			}
			if end := strings.LastIndex(s, "```"); end >= 0 {
				s = s[:end]
			}
		}
	}
	start := strings.IndexAny(s, "[{")
	if start > 0 {
		s = s[start:]
	}
	// Trim trailing prose or fence debris after the JSON value.
	for len(s) > 0 {
		var probe any
		if err := json.Unmarshal([]byte(s), &probe); err == nil {
			break
		}
		if end := strings.LastIndexAny(s, "}]"); end >= 0 && end+1 < len(s) {
			s = strings.TrimSpace(s[:end+1])
			continue
		}
		break
	}
	return strings.TrimSpace(s)
}

func parseAndValidateJSON(text string, schemaDoc map[string]any) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(stripJSONFences(text)), &value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(schemaDoc) == 0 {
		return value, nil
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("workflow-schema.json", schemaDoc); err != nil {
		return nil, fmt.Errorf("invalid response schema: %w", err)
	}
	schema, err := c.Compile("workflow-schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile response schema: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return nil, fmt.Errorf("response does not match schema: %w", err)
	}
	return value, nil
}
