// Package typecoerce provides string-to-typed-value coercion utilities.
package typecoerce

import (
	"strconv"
	"strings"
)

// ParseScalar converts a string value to its most appropriate Go type.
// Evaluation order: boolean → int → float → string (returned unchanged).
//
// When extendedBooleans is true, "yes"/"on" map to true and "no"/"off" map to false.
func ParseScalar(value string, extendedBooleans bool) any {
	lower := strings.ToLower(strings.TrimSpace(value))

	// Boolean
	switch lower {
	case "true":
		return true
	case "false":
		return false
	}
	if extendedBooleans {
		switch lower {
		case "yes", "on":
			return true
		case "no", "off":
			return false
		}
	}

	// Integer
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		// Only if the string is purely numeric (no leading zeros except "0")
		if strconv.FormatInt(i, 10) == value {
			return int(i)
		}
	}

	// Float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		// Ensure it's not already parsed as int (has decimal point or exponent)
		if strings.ContainsAny(value, ".eE") {
			return f
		}
	}

	return value
}
