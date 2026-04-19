// Package shared contains helpers reused across cmd subpackages.
package shared

import (
	"encoding/json"
	"strings"
)

// ResolveExit classifies a resolver error into a CLI exit code.
//
//	3 = not found, 4 = ambiguous, 1 = other.
func ResolveExit(err error) int {
	if err == nil {
		return 0
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "ambiguous"):
		return 4
	case strings.Contains(s, "not found"), strings.Contains(s, "no "):
		return 3
	}
	return 1
}

// PickID extracts a nested string from a JSON body by key-path.
// Integer path segments index into arrays.
func PickID(body []byte, keys ...any) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	cur := v
	for _, k := range keys {
		switch key := k.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return ""
			}
			cur = m[key]
		case int:
			arr, ok := cur.([]any)
			if !ok || key < 0 || key >= len(arr) {
				return ""
			}
			cur = arr[key]
		default:
			return ""
		}
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}
