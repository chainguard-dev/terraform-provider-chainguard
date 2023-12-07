/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package protoutil

import "github.com/hashicorp/terraform-plugin-framework/types"

// FirstNonEmpty returns the first string that is non-empty.
// If all strings are empty, the empty string is returned.
func FirstNonEmpty(sl ...string) string {
	for _, s := range sl {
		if s != "" {
			return s
		}
	}
	return ""
}

// DefaultBool returns either the value of b if non-null, or d.
func DefaultBool(b types.Bool, d bool) bool {
	if !b.IsNull() {
		return b.ValueBool()
	}
	return d
}
