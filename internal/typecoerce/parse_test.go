package typecoerce

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseScalar(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		extendedBooleans bool
		want             any
	}{
		{"true", "true", false, true},
		{"false", "false", false, false},
		{"TRUE", "TRUE", false, true},
		{"FALSE", "FALSE", false, false},
		{"yes without extended", "yes", false, "yes"},
		{"yes with extended", "yes", true, true},
		{"no with extended", "no", true, false},
		{"on with extended", "on", true, true},
		{"off with extended", "off", true, false},
		{"integer", "42", false, 42},
		{"negative integer", "-1", false, -1},
		{"zero", "0", false, 0},
		{"float", "3.14", false, 3.14},
		{"negative float", "-0.5", false, -0.5},
		{"string", "hello", false, "hello"},
		{"empty string", "", false, ""},
		{"leading zeros preserved as string", "007", false, "007"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseScalar(tt.input, tt.extendedBooleans)
			assert.Equal(t, tt.want, got)
		})
	}
}
