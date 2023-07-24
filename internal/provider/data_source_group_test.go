/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import "fmt"

func dataGroupTest(name, id string) string {
	const tmpl = `
data "chainguard_group" "test" {
	name = %q
	parent_id = %q
}
`
	return fmt.Sprintf(tmpl, name, id)
}

// TODO(colin): for this to work, also need to provide a test group name, or query the api first :thinking:
