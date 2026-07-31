// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package formatparse

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ValidateDeclaredContent rejects content that conclusively belongs to a
// different format than the one explicitly selected by the caller. JSON has a
// reliable whole-document signature, while YAML and some key/value formats can
// overlap syntactically. A selected JSON parser remains authoritative; any
// other selected format rejects a complete JSON document before parsing.
func ValidateDeclaredContent(format Format, data []byte) error {
	if format == FormatUnknown || format == FormatJSON {
		return nil
	}

	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) > 0 && json.Valid(trimmed) {
		return fmt.Errorf("declared %s source contains a JSON document; select JSON and use a .json path", format)
	}
	return nil
}
