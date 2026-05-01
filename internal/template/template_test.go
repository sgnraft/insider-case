package template_test

import (
	"testing"

	tmpl "github.com/sgnraft/insider-case/internal/template"
)

func TestRender_BasicSubstitution(t *testing.T) {
	content := "Hello {{name}}, your code is {{code}}"
	vars := map[string]interface{}{
		"name": "Alice",
		"code": "123456",
	}

	result, err := tmpl.Render(content, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Hello Alice, your code is 123456"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestRender_MissingVariable(t *testing.T) {
	content := "Hello {{name}}, click {{link}}"
	vars := map[string]interface{}{
		"name": "Bob",
	}

	_, err := tmpl.Render(content, vars)
	if err == nil {
		t.Error("expected error for missing variable, got nil")
	}
}

func TestRender_NoVariables(t *testing.T) {
	content := "Static message with no placeholders"
	result, err := tmpl.Render(content, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Errorf("got %q, want %q", result, content)
	}
}

func TestVariables_Extract(t *testing.T) {
	content := "Dear {{first_name}} {{last_name}}, your order {{order_id}} is ready"
	vars := tmpl.Variables(content)

	expected := map[string]bool{
		"first_name": true,
		"last_name":  true,
		"order_id":   true,
	}
	if len(vars) != len(expected) {
		t.Errorf("got %d variables, want %d", len(vars), len(expected))
	}
	for _, v := range vars {
		if !expected[v] {
			t.Errorf("unexpected variable: %s", v)
		}
	}
}

func TestRender_IntegerVariable(t *testing.T) {
	content := "Your balance is {{amount}} TL"
	vars := map[string]interface{}{
		"amount": 500,
	}
	result, err := tmpl.Render(content, vars)
	if err != nil {
		t.Fatal(err)
	}
	expected := "Your balance is 500 TL"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}
