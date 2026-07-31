// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package configdecode

import (
	"testing"

	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapCanonicalBehavior(t *testing.T) {
	tests := []struct {
		name   string
		format formatparse.Format
		data   string
		want   map[string]any
	}{
		{name: "yaml", format: formatparse.FormatYAML, data: "server:\n  port: 8080\n", want: map[string]any{"server": map[string]any{"port": 8080}}},
		{name: "json", format: formatparse.FormatJSON, data: `{"server":{"port":8080}}`, want: map[string]any{"server": map[string]any{"port": float64(8080)}}},
		{name: "toml", format: formatparse.FormatTOML, data: "[server]\nport = 8080\n", want: map[string]any{"server": map[string]any{"port": int64(8080)}}},
		{name: "ini", format: formatparse.FormatINI, data: "enabled=true\n[server]\nport=8080\n", want: map[string]any{"enabled": true, "server": map[string]any{"port": 8080}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Map([]byte(test.data), test.format)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestMapRejectsNonMappingMultipleAndCrossFormatDocuments(t *testing.T) {
	for _, test := range []struct {
		name   string
		format formatparse.Format
		data   string
	}{
		{name: "yaml sequence", format: formatparse.FormatYAML, data: "- one\n- two\n"},
		{name: "json sequence", format: formatparse.FormatJSON, data: `[1,2]`},
		{name: "multiple yaml", format: formatparse.FormatYAML, data: "a: 1\n---\nb: 2\n"},
		{name: "multiple json", format: formatparse.FormatJSON, data: `{"a":1} {"b":2}`},
		{name: "malformed trailing json", format: formatparse.FormatJSON, data: `{"a":1} {`},
		{name: "json declared yaml", format: formatparse.FormatYAML, data: `{"a":1}`},
		{name: "json declared toml", format: formatparse.FormatTOML, data: `{"a":1}`},
		{name: "invalid toml", format: formatparse.FormatTOML, data: "value = [\n"},
		{name: "invalid ini", format: formatparse.FormatINI, data: "\x00\x01\x02"},
		{name: "unsupported", format: formatparse.FormatUnknown, data: "a: 1\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Map([]byte(test.data), test.format)
			require.Error(t, err)
		})
	}
}

func TestAutoMapUsesJSONThenYAML(t *testing.T) {
	jsonResult, err := AutoMap([]byte(`{"server":{"port":8080}}`))
	require.NoError(t, err)
	assert.Equal(t, float64(8080), jsonResult["server"].(map[string]any)["port"])

	got, err := AutoMap([]byte("server:\n  port: 8080\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"server": map[string]any{"port": 8080}}, got)

	_, err = AutoMap([]byte("[broken"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON parse")
	assert.Contains(t, err.Error(), "YAML parse")
}

func TestMapEmptyDocumentsAndNormalizationFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		format formatparse.Format
		data   string
	}{
		{name: "empty yaml", format: formatparse.FormatYAML},
		{name: "null yaml", format: formatparse.FormatYAML, data: "~\n"},
		{name: "null json", format: formatparse.FormatJSON, data: "null"},
		{name: "empty ini", format: formatparse.FormatINI},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Map([]byte(test.data), test.format)
			require.NoError(t, err)
			assert.Nil(t, got)
		})
	}

	_, err := Map([]byte("null: value\n"), formatparse.FormatYAML)
	require.Error(t, err)
}
