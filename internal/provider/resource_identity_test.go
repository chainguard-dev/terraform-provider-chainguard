/*
Copyright 2022 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkauth "chainguard.dev/sdk/auth"
	"chainguard.dev/sdk/proto/platform"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	"chainguard.dev/sdk/sts"
	"chainguard.dev/sdk/uidp"
)

func pattern(s string) string {
	return "^" + regexp.QuoteMeta(s) + "$"
}

func literal(s string) *regexp.Regexp {
	return regexp.MustCompile(s)
}

func checkRegexp(r string) error {
	_, err := regexp.Compile(r)
	return err
}

func TestAccResourceClaimMatchIdentity(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	issuer := "https://accounts.google.com"
	subject := "robot@my-project.iam.gserviceaccount.com"
	claimPatterns := map[string]string{
		"email": ".*@chainguard.dev",
	}
	newClaimPatterns := map[string]string{
		"email": ".+@chainguard.dev",
		"group": "^security@chainguard.dev$",
	}
	claims := map[string]string{
		"email": "nghia@chainguard.dev",
	}
	newClaims := map[string]string{
		"email": "t@chainguard.dev",
		"group": "security@chainguard.dev",
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccResourceIdentityClaimMatch(group, "bill", pattern(issuer), pattern(subject), "something", claims, claimPatterns),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer_pattern`, pattern(issuer)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.issuer_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject_pattern`, pattern(subject)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.subject_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.audience_pattern`, pattern("something")),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.audience_pattern", checkRegexp),

					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.claim_patterns.email`, literal(".*@chainguard.dev")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.claims.email`, literal("nghia@chainguard.dev")),
				),
			},
			// Test ImportState once.
			{
				ResourceName:      "chainguard_identity.user",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update
			{
				Config: testAccResourceIdentityClaimMatch(group, "bill", pattern(issuer), pattern(subject), "something", newClaims, newClaimPatterns),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer_pattern`, pattern(issuer)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.issuer_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject_pattern`, pattern(subject)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.subject_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.audience_pattern`, pattern("something")),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.audience_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.claim_patterns.email`, newClaimPatterns["email"]),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.claim_patterns.email", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.claim_patterns.group`, newClaimPatterns["group"]),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.claim_patterns.group", checkRegexp),

					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.claims.email`, literal("t@chainguard.dev")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.claims.group`, literal("security@chainguard.dev")),
				),
			},
		},
	})
}

func TestAccResourceLiteralIdentity(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	issuer := "https://accounts.google.com"
	newIssuer := "https://token.githubusercontent.com"
	subject := "robot@my-project.iam.gserviceaccount.com"
	newSubject := "android@my-project.iam.gserviceaccount.com"

	// Check changing names.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `name`, "bill"),
				),
			},
			{
				Config: testAccResourceIdentityLiteral(group, "ted", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `name`, "ted"),
				),
			},
		},
	})

	// Check changing issuer.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, issuer),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject`, subject),
				),
			},
			{
				Config: testAccResourceIdentityLiteral(group, "bill", newIssuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, newIssuer),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject`, subject),
				),
			},
		},
	})

	// Check changing subject.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, issuer),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject`, subject),
				),
			},
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, newSubject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, issuer),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject`, newSubject),
				),
			},
		},
	})

	// Check changing literals to patterns.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, issuer),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject`, subject),
				),
			},
			{
				Config: testAccResourceIdentityClaimMatch(group, "bill", pattern(issuer), pattern(subject), "something", nil /*claims*/, nil /* claimPatterns */),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.issuer_pattern`, pattern(issuer)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.issuer_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.subject_pattern`, pattern(subject)),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.subject_pattern", checkRegexp),

					resource.TestCheckResourceAttr(`chainguard_identity.user`, `claim_match.audience_pattern`, pattern("something")),
					resource.TestCheckResourceAttrWith("chainguard_identity.user", "claim_match.audience_pattern", checkRegexp),

					// The twin for claim matches use an exact aud
					resource.TestCheckResourceAttr(`chainguard_identity.twin`, `claim_match.audience`, "something"),
				),
			},
		},
	})
}

func TestAccResourceStaticIdentity(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	issuer := "https://accounts.google.com"
	newIssuer := "https://token.githubusercontent.com"
	subject := "robot@my-project.iam.gserviceaccount.com"
	newSubject := "android@my-project.iam.gserviceaccount.com"
	issuerKeys := "keys"
	newIssuerKeys := "keyz"
	expiration := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	newExpiration := time.Now().Add(4 * time.Hour).UTC().Format(time.RFC3339)

	// Check changing names.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "ted", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("ted")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
		},
	})

	// Check changing issuer.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", newIssuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(newIssuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
		},
	})

	// Check changing subject.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, newSubject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(newSubject)),
				),
			},
		},
	})

	// Check changing issuer_keys.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, newIssuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(newIssuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
		},
	})

	// Check changing expiration.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, newExpiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
		},
	})
}

func TestAccResourceAWSIdentity(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	const (
		awsAccount    = "123456789012"
		userID        = "foo"
		userIDPattern = "foo-pattern"
		arn           = "bar"
		arnPattern    = "bar-pattern"
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "chainguard_identity" "aws-user" {
	parent_id = %q
	name = "aws-user"
	aws_identity {
		aws_account = %q
		aws_arn = %q
		aws_user_id = %q
	}
}
`, group, awsAccount, arn, userID),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.aws-user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_account`, awsAccount),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_arn`, arn),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_user_id`, userID),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "chainguard_identity" "aws-user" {
	parent_id = %q
	name = "aws-user"
	aws_identity {
		aws_account = %q
		aws_arn_pattern = %q
		aws_user_id_pattern = %q
	}
}
`, group, awsAccount, arnPattern, userIDPattern),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.aws-user`, `id`, childpattern),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_account`, awsAccount),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_arn_pattern`, arnPattern),
					resource.TestCheckResourceAttr(`chainguard_identity.aws-user`, `aws_identity.aws_user_id_pattern`, userIDPattern),
				),
			},
		},
	})
}

func TestAccResourceServicePrincipal(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	service := "INGESTER"
	newService := "COSIGNED"

	// Check changing names.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityServicePrincipal(group, "bill", service),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(service)),
				),
			},
			{
				Config: testAccResourceIdentityServicePrincipal(group, "ted", service),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("ted")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(service)),
				),
			},
		},
	})

	// Check changing service.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityServicePrincipal(group, "bill", service),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(service)),
				),
			},
			{
				Config: testAccResourceIdentityServicePrincipal(group, "bill", newService),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(newService)),
				),
			},
		},
	})
}

func TestAccResourceIdentityTypeChange(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	childpattern := regexp.MustCompile(fmt.Sprintf(`%s\/[a-z0-9]{16}`, group))

	service := "INGESTER"
	issuer := "https://accounts.google.com"
	subject := "robot@my-project.iam.gserviceaccount.com"
	issuerKeys := "keys"
	expiration := time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339)

	// Check changing claim_match to service_principal.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityServicePrincipal(group, "bill", service),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(service)),
					resource.TestCheckNoResourceAttr(`chainguard_identity.user`, `claim_match`),
				),
			},
		},
	})

	// Check changing service_principal to static_keys.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityServicePrincipal(group, "bill", service),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `service_principal`, literal(service)),
				),
			},
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
					resource.TestCheckNoResourceAttr(`chainguard_identity.user`, `service_principal`),
				),
			},
		},
	})

	// Check changing static_keys to claim_match.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceIdentityStaticKeys(group, "bill", issuer, subject, issuerKeys, expiration),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.issuer_keys`, literal(issuerKeys)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `static.subject`, literal(subject)),
				),
			},
			{
				Config: testAccResourceIdentityLiteral(group, "bill", issuer, subject),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `id`, childpattern),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `name`, literal("bill")),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.issuer`, literal(issuer)),
					resource.TestMatchResourceAttr(`chainguard_identity.user`, `claim_match.subject`, literal(subject)),
					resource.TestCheckNoResourceAttr("chainguard_identity.user", "static"),
				),
			},
		},
	})
}

func randString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func TestAccResourceIdentityUsage(t *testing.T) {
	group := os.Getenv("TF_ACC_GROUP_ID")

	issuer := "https://justtrustme.dev"
	subject := uidp.NewUID()
	audience := uidp.NewUID()
	customClaimID := "claim_" + randString(6)
	customClaimValue := uidp.NewUID().String() + "@chainguard.dev"
	resp, err := http.Get(fmt.Sprintf("%s/token?sub=%s&aud=%s&%s=%s", issuer, subject, audience, customClaimID, customClaimValue))
	if err != nil {
		t.Fatalf("NewRequest() = %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() = %v", err)
	}

	var envelope struct {
		Token string `json:"token,omitempty"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}

	// Create the exchanger for turning the "justtrustme" tokens into Chainguard
	// tokens.
	xchg := sts.New(os.Getenv("TF_ACC_ISSUER"), os.Getenv("TF_ACC_AUDIENCE"))

	t.Run("issuer,subject,audience match", func(t *testing.T) {
		// Check changing claim_match to static_keys.
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
	audience = %q
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					tok, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err != nil {
						return err
					}

					// Use the token we get back to list groups.
					cred := sdkauth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", tok), false)
					clients, err := platform.NewPlatformClients(ctx, os.Getenv("TF_ACC_CONSOLE_API"), cred)
					if err != nil {
						return err
					}
					gl, err := clients.IAM().Groups().List(ctx, &iam.GroupFilter{})
					if err != nil {
						return err
					}
					if len(gl.Items) != 1 {
						ids := make([]string, 0, len(gl.Items))
						for _, g := range gl.Items {
							ids = append(ids, g.Id)
						}
						return fmt.Errorf("got %d groups, wanted 1: %s", len(gl.Items), strings.Join(ids, ", "))
					}
					g := gl.Items[0]
					if g.Id != group {
						return fmt.Errorf("got %q, wanted %q", g.Id, group)
					}
					return nil
				},
			}, {
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
   audience = %q
   claim_patterns = {
     %s: %q
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, pattern(customClaimValue), group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					tok, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err != nil {
						return err
					}
					verifier := gooidc.NewVerifier("https://issuer.oidc-system.svc", &gooidc.StaticKeySet{}, &gooidc.Config{
						/* We just want to parse the token, so skip almost all checks */
						SkipClientIDCheck:          true,
						InsecureSkipSignatureCheck: true,
						SkipIssuerCheck:            true,
					})
					t, err := verifier.Verify(ctx, tok)
					if err != nil {
						return err
					}
					act := struct {
						Act map[string]interface{}
					}{}
					if err = t.Claims(&act); err != nil {
						return err
					}
					if got, ok := act.Act[customClaimID].(string); !ok || got != customClaimValue {
						return fmt.Errorf("got act[%q] = %q, wanted %q", customClaimID, got, customClaimValue)
					}
					// Use the token we get back to list groups.
					cred := sdkauth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", tok), false)
					clients, err := platform.NewPlatformClients(ctx, os.Getenv("TF_ACC_CONSOLE_API"), cred)
					if err != nil {
						return err
					}
					gl, err := clients.IAM().Groups().List(ctx, &iam.GroupFilter{})
					if err != nil {
						return err
					}
					if len(gl.Items) != 1 {
						ids := make([]string, 0, len(gl.Items))
						for _, g := range gl.Items {
							ids = append(ids, g.Id)
						}
						return fmt.Errorf("got %d groups, wanted 1: %s", len(gl.Items), strings.Join(ids, ", "))
					}
					g := gl.Items[0]
					if g.Id != group {
						return fmt.Errorf("got %q, wanted %q", g.Id, group)
					}
					return nil
				},
			}, {
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
	audience = %q
   claim_patterns = {
     %s: "^dlorenc@chainguard.dev$"
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					_, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err == nil {
						return errors.New("expected err, saw none")
					}
					if code := status.Code(err); code != codes.PermissionDenied {
						return fmt.Errorf("expected error code %v, saw %v", codes.PermissionDenied, code)
					}
					expectMsg := fmt.Sprintf("invalid %q", customClaimID)
					if !strings.Contains(err.Error(), expectMsg) {
						return fmt.Errorf("expected error to contain %q, saw: %w", expectMsg, err)
					}
					return nil
				},
			}},
		})
	})

	t.Run("claim_patterns match", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
   audience = %q
   claim_patterns = {
     %s: %q
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, pattern(customClaimValue), group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					tok, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err != nil {
						return err
					}
					verifier := gooidc.NewVerifier("https://issuer.oidc-system.svc", &gooidc.StaticKeySet{}, &gooidc.Config{
						/* We just want to parse the token, so skip almost all checks */
						SkipClientIDCheck:          true,
						InsecureSkipSignatureCheck: true,
						SkipIssuerCheck:            true,
					})
					t, err := verifier.Verify(ctx, tok)
					if err != nil {
						return err
					}
					act := struct {
						Act map[string]interface{}
					}{}
					if err = t.Claims(&act); err != nil {
						return err
					}
					if got, ok := act.Act[customClaimID].(string); !ok || got != customClaimValue {
						return fmt.Errorf("got act[%q] = %q, wanted %q", customClaimID, got, customClaimValue)
					}
					// Use the token we get back to list groups.
					cred := sdkauth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", tok), false)
					clients, err := platform.NewPlatformClients(ctx, os.Getenv("TF_ACC_CONSOLE_API"), cred)
					if err != nil {
						return err
					}
					gl, err := clients.IAM().Groups().List(ctx, &iam.GroupFilter{})
					if err != nil {
						return err
					}
					if len(gl.Items) != 1 {
						ids := make([]string, 0, len(gl.Items))
						for _, g := range gl.Items {
							ids = append(ids, g.Id)
						}
						return fmt.Errorf("got %d groups, wanted 1: %s", len(gl.Items), strings.Join(ids, ", "))
					}
					g := gl.Items[0]
					if g.Id != group {
						return fmt.Errorf("got %q, wanted %q", g.Id, group)
					}
					return nil
				},
			}, {
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
	audience = %q
   claim_patterns = {
     %s: "^dlorenc@chainguard.dev$"
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					_, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err == nil {
						return errors.New("expected err, saw none")
					}
					if code := status.Code(err); code != codes.PermissionDenied {
						return fmt.Errorf("expected error code %v, saw %v", codes.PermissionDenied, code)
					}
					expectMsg := fmt.Sprintf("invalid %q", customClaimID)
					if !strings.Contains(err.Error(), expectMsg) {
						return fmt.Errorf("expected error to contain %q, saw: %w", expectMsg, err)
					}
					return nil
				},
			}},
		})
	})

	t.Run("claims match", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
   audience = %q
   claims = {
     %s: %q
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, customClaimValue, group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					tok, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err != nil {
						return err
					}
					verifier := gooidc.NewVerifier("https://issuer.oidc-system.svc", &gooidc.StaticKeySet{}, &gooidc.Config{
						/* We just want to parse the token, so skip almost all checks */
						SkipClientIDCheck:          true,
						InsecureSkipSignatureCheck: true,
						SkipIssuerCheck:            true,
					})
					t, err := verifier.Verify(ctx, tok)
					if err != nil {
						return err
					}
					act := struct {
						Act map[string]interface{}
					}{}
					if err = t.Claims(&act); err != nil {
						return err
					}
					if got, ok := act.Act[customClaimID].(string); !ok || got != customClaimValue {
						return fmt.Errorf("got act[%q] = %q, wanted %q", customClaimID, got, customClaimValue)
					}
					// Use the token we get back to list groups.
					cred := sdkauth.NewFromToken(ctx, fmt.Sprintf("Bearer %s", tok), false)
					clients, err := platform.NewPlatformClients(ctx, os.Getenv("TF_ACC_CONSOLE_API"), cred)
					if err != nil {
						return err
					}
					gl, err := clients.IAM().Groups().List(ctx, &iam.GroupFilter{})
					if err != nil {
						return err
					}
					if len(gl.Items) != 1 {
						ids := make([]string, 0, len(gl.Items))
						for _, g := range gl.Items {
							ids = append(ids, g.Id)
						}
						return fmt.Errorf("got %d groups, wanted 1: %s", len(gl.Items), strings.Join(ids, ", "))
					}
					g := gl.Items[0]
					if g.Id != group {
						return fmt.Errorf("got %q, wanted %q", g.Id, group)
					}
					return nil
				},
			}, {
				Config: fmt.Sprintf(`
data "chainguard_role" "owner" {
 name = "owner"
}

resource "chainguard_identity" "user" {
 parent_id = %q
 name      = %q
 claim_match {
   issuer   = %q
   subject  = %q
	audience = %q
   claims = {
     %s: "dlorenc@chainguard.dev"
   }
 }
}

resource "chainguard_rolebinding" "binding" {
 identity = chainguard_identity.user.id
 group    = %q
 role     = data.chainguard_role.owner.items.0.id
}
`, group, "test", issuer, subject, audience, customClaimID, group),
				Check: func(s *terraform.State) error {
					ctx := context.Background()

					// Assume the resulting identity.
					rs := s.RootModule().Resources["chainguard_identity.user"]
					_, err := xchg.Exchange(ctx, envelope.Token, sts.WithIdentity(rs.Primary.ID))
					if err == nil {
						return errors.New("expected err, saw none")
					}
					if code := status.Code(err); code != codes.PermissionDenied {
						return fmt.Errorf("expected error code %v, saw %v", codes.PermissionDenied, code)
					}
					expectMsg := fmt.Sprintf("invalid %q", customClaimID)
					if !strings.Contains(err.Error(), expectMsg) {
						return fmt.Errorf("expected error to contain %q, saw: %w", expectMsg, err)
					}
					return nil
				},
			}},
		})
	})
}

func testAccResourceIdentityLiteral(group, name, issuer, subject string) string {
	tmpl := `
resource "chainguard_identity" "user" {
parent_id = %q
name      = %q
claim_match {
  issuer  = %q
  subject = %q
}
}

// We should only have a uniqueness constraint on top-level (iss, sub) pairs.
resource "chainguard_identity" "twin" {
parent_id = %q
name      = %q
claim_match {
  issuer  = %q
  subject = %q
}
}
`
	return fmt.Sprintf(
		tmpl,
		group,
		name,
		issuer,
		subject,
		group,
		name,
		issuer,
		subject,
	)
}

func testAccResourceIdentityClaimMatch(group, name, issuerPattern, subjectPattern, audience string, claims, claimPatterns map[string]string) string {
	tmpl := `
resource "chainguard_identity" "user" {
parent_id = %q
name      = %q
claim_match {
  issuer_pattern   = %q
  subject_pattern  = %q
  audience_pattern = %q
  claims = {
    %s
  }
  claim_patterns = {
    %s
  }
}
}

resource "chainguard_identity" "twin" {
parent_id = %q
name      = %q
claim_match {
  issuer_pattern  = %q
  subject_pattern = %q
  audience        = %q
  claims = {
    %s
  }
  claim_patterns = {
    %s
  }
}
}
`
	claimsStr := &strings.Builder{}
	for k, v := range claims {
		fmt.Fprintf(claimsStr, "%q = %q\n", k, v)
	}
	claimPatternsStr := &strings.Builder{}
	for k, v := range claimPatterns {
		fmt.Fprintf(claimPatternsStr, "%q = %q\n", k, v)
	}
	return fmt.Sprintf(
		tmpl,
		group,
		name,
		issuerPattern,
		subjectPattern,
		pattern(audience),
		claimsStr.String(),
		claimPatternsStr.String(),
		group,
		name,
		issuerPattern,
		subjectPattern,
		audience,
		claimsStr.String(),
		claimPatternsStr.String(),
	)
}

func testAccResourceIdentityStaticKeys(group, name, issuer, subject, issuerKeys, expiration string) string {
	tmpl := `
resource "chainguard_identity" "user" {
parent_id = %q
name      = %q
static {
  issuer      = %q
  subject     = %q
	issuer_keys = %q
	expiration  = %q
}
}
`
	return fmt.Sprintf(
		tmpl,
		group,
		name,
		issuer,
		subject,
		issuerKeys,
		expiration,
	)
}

func testAccResourceIdentityServicePrincipal(group, name, service string) string {
	tmpl := `
resource "chainguard_identity" "user" {
parent_id         = %q
name              = %q
service_principal = %q
}
`
	return fmt.Sprintf(
		tmpl,
		group,
		name,
		service,
	)
}
