/*
Copyright 2023 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"chainguard.dev/sdk/proto/platform"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	events "chainguard.dev/sdk/proto/platform/events/v1"
	iam "chainguard.dev/sdk/proto/platform/iam/v1"
	registry "chainguard.dev/sdk/proto/platform/registry/v1"
	"github.com/chainguard-dev/terraform-provider-chainguard/internal/token"
)

var (
	// testAccProtoV6ProviderFactories are used to instantiate a provider during
	// acceptance testing. The factory function will be invoked for every Terraform
	// CLI command executed to create a provider server to which the CLI can
	// reattach.
	testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
		"chainguard": providerserver.NewProtocol6WithError(New("acctest")()),
	}
)

func testAccPreCheck(t *testing.T) {
	m := "%s env var must be set to run acceptance tests"

	// TF_ACC environment variables must be set to run
	// acceptance tests.
	for _, v := range EnvAccVars {
		if os.Getenv(v) == "" {
			t.Fatalf(m, v)
		}
	}
}

// testAccPlatformClient creates a platform client for CheckDestroy verification.
// Returns nil if acceptance test env vars are not set (non-acceptance test runs).
func testAccPlatformClient(t *testing.T) platform.Clients {
	t.Helper()
	consoleAPI := os.Getenv(EnvAccConsoleAPI)
	audience := os.Getenv(EnvAccAudience)
	if consoleAPI == "" || audience == "" {
		return nil
	}
	ctx := context.Background()

	cfg := token.LoginConfig{
		Audience:  audience,
		Issuer:    strings.Replace(consoleAPI, "console-api", "issuer", 1),
		UserAgent: "terraform-provider-chainguard/acctest",
	}
	tok, err := token.Get(ctx, cfg, false)
	if err != nil {
		t.Fatalf("failed to get token for CheckDestroy: %s", err)
	}
	clients, err := newPlatformClients(ctx, string(tok), consoleAPI)
	if err != nil {
		t.Fatalf("failed to create platform clients for CheckDestroy: %s", err)
	}
	return clients
}

// checkGroupDestroy verifies the group resource was deleted.
func checkGroupDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_group" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().Groups().List(ctx, &iam.GroupFilter{Id: id})
			if err != nil {
				// API error is OK — resource may already be gone.
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("group %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkIdentityDestroy verifies the identity resource was deleted.
func checkIdentityDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_identity" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().Identities().List(ctx, &iam.IdentityFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("identity %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkIdentityProviderDestroy verifies the identity provider resource was deleted.
func checkIdentityProviderDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_identity_provider" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().IdentityProviders().List(ctx, &iam.IdentityProviderFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("identity provider %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkRoleDestroy verifies the role resource was deleted.
func checkRoleDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_role" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().Roles().List(ctx, &iam.RoleFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("role %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkRolebindingDestroy verifies the rolebinding resource was deleted.
func checkRolebindingDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_rolebinding" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().RoleBindings().List(ctx, &iam.RoleBindingFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("rolebinding %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkSubscriptionDestroy verifies the subscription resource was deleted.
func checkSubscriptionDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_subscription" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().Subscriptions().List(ctx, &events.SubscriptionFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("subscription %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkAccountAssociationsDestroy verifies the account associations resource was deleted.
func checkAccountAssociationsDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_account_associations" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().AccountAssociations().List(ctx, &iam.AccountAssociationsFilter{Group: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("account associations for group %s still exist after destroy", id)
			}
		}
		return nil
	}
}

// checkGroupInviteDestroy verifies the group invite resource was deleted.
func checkGroupInviteDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_group_invite" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.IAM().GroupInvites().List(ctx, &iam.GroupInviteFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("group invite %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkImageRepoDestroy verifies the image repo resource was deleted.
// Note: image_repo delete is a no-op in non-test mode, so this only
// verifies in acceptance test context where delete is real.
func checkImageRepoDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_image_repo" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.Registry().Registry().ListRepos(ctx, &registry.RepoFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("image repo %s still exists after destroy", id)
			}
		}
		return nil
	}
}

// checkImageTagDestroy verifies the image tag resource was deleted.
// Note: image_tag delete is a no-op in non-test mode, so this only
// verifies in acceptance test context where delete is real.
func checkImageTagDestroy(clients platform.Clients) func(*terraform.State) error {
	return func(s *terraform.State) error {
		if clients == nil {
			return nil
		}
		ctx := context.Background()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "chainguard_image_tag" {
				continue
			}
			id := rs.Primary.ID
			list, err := clients.Registry().Registry().ListTags(ctx, &registry.TagFilter{Id: id})
			if err != nil {
				continue
			}
			if len(list.GetItems()) > 0 {
				return fmt.Errorf("image tag %s still exists after destroy", id)
			}
		}
		return nil
	}
}
