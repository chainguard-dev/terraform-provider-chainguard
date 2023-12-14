/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package protoutil

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
