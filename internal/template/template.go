package template

import (
	"fmt"
	"regexp"
	"strings"
)

var varPattern = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Render substitutes {{variable}} placeholders in a template content string
func Render(content string, vars map[string]interface{}) (string, error) {
	var missingVars []string

	result := varPattern.ReplaceAllStringFunc(content, func(match string) string {
		key := varPattern.FindStringSubmatch(match)[1]
		val, ok := vars[key]
		if !ok {
			missingVars = append(missingVars, key)
			return match
		}
		return fmt.Sprintf("%v", val)
	})

	if len(missingVars) > 0 {
		return result, fmt.Errorf("missing template variables: %s", strings.Join(missingVars, ", "))
	}
	return result, nil
}

// Variables extracts variable names from a template
func Variables(content string) []string {
	matches := varPattern.FindAllStringSubmatch(content, -1)
	vars := make([]string, 0, len(matches))
	for _, m := range matches {
		vars = append(vars, m[1])
	}
	return vars
}
