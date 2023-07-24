/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package protoutil

import (
	"sort"
	"strings"

	"golang.org/x/exp/maps"
)

func EnumToQuotedString(enum map[string]int32) string {
	// Sort for stable output
	keys := maps.Keys(enum)
	sort.Strings(keys)
	return `"` + strings.Join(keys, `", "`) + `"`
}
