package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"approval-engine-service/internal/domain"
)

// EvaluateCondition decides whether a step should run, based on the payload
// the consuming application sent. A nil condition always runs.
//
// This is deliberately a small fixed set of operators rather than an
// expression language: it covers the real cases (amount thresholds, category
// matching) without anyone having to learn or debug a custom syntax.
func EvaluateCondition(c *domain.Condition, payload map[string]any) (bool, error) {
	if c == nil {
		return true, nil
	}

	actual, present := payload[c.Field]
	if !present {
		// A condition on a field the caller did not send is treated as unmet
		// rather than an error, so one missing optional field cannot break a
		// whole workflow.
		return false, nil
	}

	switch c.Op {
	case "eq":
		return looseEqual(actual, c.Value), nil
	case "ne":
		return !looseEqual(actual, c.Value), nil
	case "gt", "gte", "lt", "lte":
		return compareNumbers(c.Op, actual, c.Value)
	case "in":
		list, ok := c.Value.([]any)
		if !ok {
			return false, fmt.Errorf("operator %q needs an array value", c.Op)
		}
		for _, item := range list {
			if looseEqual(actual, item) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unknown operator %q", c.Op)
	}
}

func compareNumbers(op string, actual, expected any) (bool, error) {
	// These operators are numeric by definition, so a numeric string is
	// accepted on both sides: thresholds typed into the workflow admin form
	// arrive as strings.
	a, ok := toNumber(actual)
	if !ok {
		return false, fmt.Errorf("value %v is not a number", actual)
	}
	b, ok := toNumber(expected)
	if !ok {
		return false, fmt.Errorf("condition value %v is not a number", expected)
	}

	switch op {
	case "gt":
		return a > b, nil
	case "gte":
		return a >= b, nil
	case "lt":
		return a < b, nil
	default:
		return a <= b, nil
	}
}

// looseEqual compares numerically when at least one side is a real number, so
// an amount of 75000000 matches a threshold of "75000000" typed into the admin
// form. Two strings are always compared as strings, so ids like "00123" and
// "123" stay different.
func looseEqual(a, b any) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok := toNumber(b); ok {
			return af == bf
		}
	}
	if bf, ok := toFloat(b); ok {
		if af, ok := toNumber(a); ok {
			return af == bf
		}
	}
	return formatValue(a) == formatValue(b)
}

// toFloat accepts genuine numeric types only.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// toNumber also accepts a numeric string.
func toNumber(v any) (float64, bool) {
	if f, ok := toFloat(v); ok {
		return f, true
	}
	if s, ok := v.(string); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, err == nil
	}
	return 0, false
}

// formatValue keeps large numbers readable instead of "7.5e+07".
func formatValue(v any) string {
	if f, ok := toFloat(v); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}
