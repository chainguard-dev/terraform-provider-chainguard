/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package protoutil

import (
	"testing"
)

func TestEnumToQuotedString(t *testing.T) {
	tests := map[string]struct {
		Input  map[string]int32
		Expect string
	}{
		"empty": {
			Input:  map[string]int32{},
			Expect: `""`,
		},
		"simple": {
			Input: map[string]int32{
				"first":  1,
				"second": 2,
			},
			Expect: `"first", "second"`,
		},
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			got := EnumToQuotedString(data.Input)
			if got != data.Expect {
				t.Errorf("got %s, expected %s", got, data.Expect)
			}
		})
	}
}
