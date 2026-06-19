package workflow

import "testing"

func TestParseAndValidateJSON(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"ok"},
		"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
	}
	value, err := parseAndValidateJSON("```json\n{\"ok\":true}\n```", schema)
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["ok"] != true {
		t.Fatalf("value = %#v", value)
	}
	if _, err := parseAndValidateJSON(`{"ok":"yes"}`, schema); err == nil {
		t.Fatal("expected schema rejection")
	}
	prose := "Based on my analysis, here is the JSON report:\n\n```json\n{\"ok\":true}\n```"
	value, err = parseAndValidateJSON(prose, schema)
	if err != nil {
		t.Fatalf("prose before fence: %v", err)
	}
	if value.(map[string]any)["ok"] != true {
		t.Fatalf("value = %#v", value)
	}
}
