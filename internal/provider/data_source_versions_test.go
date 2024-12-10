/*
Copyright 2024 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	registrytest "chainguard.dev/sdk/proto/platform/registry/v1/test"
	platformtest "chainguard.dev/sdk/proto/platform/test"
)

func Test_calculate(t *testing.T) {
	clients := &platformtest.MockPlatformClients{
		RegistryClient: registrytest.MockRegistryClients{
			RegistryClient: registrytest.MockRegistryClient{
				OnGetPackageVersionMetadata: []registrytest.PackageVersionMetadataOnGet{
					{
						Given: &registry.PackageVersionMetadataRequest{
							Package: "bad",
						},
						Error: errors.New("what is the meaning of it all"),
					},
					{
						Given: &registry.PackageVersionMetadataRequest{
							Package: "missing",
						},
						Error: status.Error(codes.NotFound, "blahhhh"),
					},
					{
						Given: &registry.PackageVersionMetadataRequest{
							Package: "found",
						},
						Get: &registry.PackageVersionMetadata{
							GracePeriodMonths: 6,
							EolVersions: []*registry.PackageVersion{
								{
									EolDate: "2924-10-07", // TODO: update in 900 years
									Exists:  true,
									Fips:    true,
									Version: "3.8",
								},
								{
									EolDate: "2001-06-27",
									Exists:  true,
									Version: "3.7",
								},
								{
									EolDate: "2924-10-07", // TODO: update in 900 years
									Exists:  true,
									Version: "3.6",
									// this should cause this version to be ignored completely
									// even though it falls into the grace period
									EolBroken: true,
								},
							},
							Versions: []*registry.PackageVersion{
								{
									EolDate: "2929-10-31",
									Exists:  true,
									Fips:    true,
									Version: "3.13",
								},
								{
									EolDate: "2928-10-31",
									Exists:  true,
									Version: "3.12",
								},
								{
									EolDate: "2925-10-31",
									Exists:  true,
									Fips:    true,
									Version: "3.9",
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name                string
		pkg                 string
		variant             string
		wantError           bool
		expectedOrderedKeys []string
		expectedVersionsMap map[string]versionsDataSourceVersionMapModel
		allow               map[string]struct{}
	}{
		{
			name:      "causes server error",
			pkg:       "bad",
			wantError: true,
		},
		{
			name:      "bad variant",
			pkg:       "found",
			variant:   "abc",
			wantError: true,
		},
		{
			name: "package not found",
			pkg:  "missing",
			expectedOrderedKeys: []string{
				"missing",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"missing": {
					Exists:   true,
					IsLatest: true,
					Main:     "missing",
					Version:  "",
				},
			},
		},
		{
			name:    "package not found, fips",
			pkg:     "missing",
			variant: "fips",
			expectedOrderedKeys: []string{
				"missing-fips",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"missing-fips": {
					Exists:   true,
					Fips:     true,
					IsLatest: true,
					Main:     "missing-fips",
					Version:  "",
				},
			},
		},
		{
			name: "happy path",
			pkg:  "found",
			expectedOrderedKeys: []string{
				"found-3.8",
				"found-3.9",
				"found-3.12",
				"found-3.13",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"found-3.8": {
					Exists:   true,
					Fips:     true,
					IsLatest: false,
					Main:     "found-3.8",
					Version:  "3.8",
					Eol:      true,
					EolDate:  "2924-10-07",
				},
				"found-3.9": {
					Exists:   true,
					Fips:     true,
					IsLatest: false,
					Main:     "found-3.9",
					Version:  "3.9",
				},
				"found-3.12": {
					Exists:   true,
					IsLatest: false,
					Main:     "found-3.12",
					Version:  "3.12",
				},
				"found-3.13": {
					Exists:   true,
					Fips:     true,
					IsLatest: true,
					Main:     "found-3.13",
					Version:  "3.13",
				},
			},
		},
		{
			name:    "happy path, fips",
			pkg:     "found",
			variant: "fips",
			expectedOrderedKeys: []string{
				"found-fips-3.8",
				"found-fips-3.9",
				"found-fips-3.13",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"found-fips-3.8": {
					Exists:   true,
					Fips:     true,
					IsLatest: false,
					Main:     "found-fips-3.8",
					Version:  "3.8",
					Eol:      true,
					EolDate:  "2924-10-07",
				},
				"found-fips-3.9": {
					Exists:   true,
					Fips:     true,
					IsLatest: false,
					Main:     "found-fips-3.9",
					Version:  "3.9",
				},
				"found-fips-3.13": {
					Exists:   true,
					Fips:     true,
					IsLatest: true,
					Main:     "found-fips-3.13",
					Version:  "3.13",
				},
			},
		},
		{
			name: "allow list",
			pkg:  "found",
			allow: map[string]struct{}{
				"found-3.8":  {},
				"found-3.13": {},
			},
			expectedOrderedKeys: []string{
				"found-3.8",
				"found-3.13",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"found-3.8": {
					Exists:   true,
					Fips:     true,
					IsLatest: false,
					Main:     "found-3.8",
					Version:  "3.8",
					Eol:      true,
					EolDate:  "2924-10-07",
				},
				"found-3.13": {
					Exists:   true,
					Fips:     true,
					IsLatest: true,
					Main:     "found-3.13",
					Version:  "3.13",
				},
			},
		},
		{
			name:    "allow list, fips",
			pkg:     "found",
			variant: "fips",
			allow: map[string]struct{}{
				"found-fips-3.9": {},
			},
			expectedOrderedKeys: []string{
				"found-fips-3.9",
			},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{
				"found-fips-3.9": {
					Exists:   true,
					Fips:     true,
					IsLatest: true, // ensure new latest is also identified
					Main:     "found-fips-3.9",
					Version:  "3.9",
				},
			},
		},
		{
			// NOTE: The provider Configure() doesn't set (is nil) when the allow
			// list isn't set, so it isn't this test, this test simply protects
			// against unknown regressions
			name:                "empty but present allow list produces nothing",
			pkg:                 "found",
			allow:               map[string]struct{}{},
			expectedOrderedKeys: []string{},
			expectedVersionsMap: map[string]versionsDataSourceVersionMapModel{},
		},
	}

	ctx := context.Background()
	testClient := clients.Registry().Registry()

	for _, test := range tests {
		_, versionsMap, orderedKeys, diagnostic := calculate(ctx, testClient, test.pkg, test.variant, test.allow)
		if !diagnostic.HasError() && test.wantError {
			t.Errorf("%s: wanted error/diag returned but was nil", test.name)
			continue
		}
		if diagnostic.HasError() && !test.wantError {
			t.Errorf("%s: error/diag returned but expected nil: %s", test.name, diagnostic.Errors())
			continue
		}
		if diff := cmp.Diff(test.expectedOrderedKeys, orderedKeys); diff != "" {
			t.Errorf("%s: orderedKeys did not match: %s", test.name, diff)
		}
		if diff := cmp.Diff(test.expectedVersionsMap, versionsMap); diff != "" {
			t.Errorf("%s: versionsMap did not match: %s", test.name, diff)
		}
	}
}
