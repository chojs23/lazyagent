package jsonutil

import (
	"encoding/json"
	"fmt"
	"time"
)

func String(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func Map(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func MapOrEmpty(v any) map[string]any {
	if m := Map(v); m != nil {
		return m
	}
	return map[string]any{}
}

func GetString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}

	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%v", val)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

func FirstPresent(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func TimestampMillis(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed.UnixMilli()
		}
	}
	return time.Now().UnixMilli()
}
