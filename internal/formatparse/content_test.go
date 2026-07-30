// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package formatparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDeclaredContentRejectsJSONForNonJSONFormats(t *testing.T) {
	for _, format := range []Format{FormatYAML, FormatTOML, FormatINI, FormatEnvFile} {
		for _, data := range [][]byte{
			[]byte(`{"server":{"port":8080}}`),
			[]byte("\xef\xbb\xbf  {\"enabled\":true}\n"),
			[]byte(`[1,2,3]`),
		} {
			err := ValidateDeclaredContent(format, data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "JSON document")
		}
	}
}

func TestValidateDeclaredContentAcceptsSelectedSyntax(t *testing.T) {
	require.NoError(t, ValidateDeclaredContent(FormatYAML, []byte("server:\n  port:8080\n")))
	require.NoError(t, ValidateDeclaredContent(FormatYAML, []byte("{server:{port:8080}}")))
	require.NoError(t, ValidateDeclaredContent(FormatJSON, []byte(`{"server":{"port":8080}}`)))
	require.NoError(t, ValidateDeclaredContent(FormatTOML, []byte("port = 8080\n")))
}
