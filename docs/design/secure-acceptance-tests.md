# Secure Acceptance Tests on Pull Requests

## Problem

The Terraform provider acceptance tests require OIDC credentials (via `setup-chainctl`)
to authenticate against the Chainguard API. Currently the workflow uses `pull_request_target`
which either:
- Checks out the base branch (tests don't run against PR code)
- Checks out the PR head (untrusted code runs with OIDC credentials)

## Solution: GitHub Environment Protection Rules

Use a `pull_request` trigger (safe — runs in PR context) combined with a GitHub Environment
that has required reviewers. The environment acts as the trust boundary: credentials are
only available after a maintainer approves the workflow run.

### How it works

1. PR is opened → workflow triggers with `pull_request` event
2. GitHub shows the job as "Waiting for review" in the checks tab
3. A required reviewer (customer-platform-team-write) clicks "Approve and deploy"
4. The job runs with `id-token: write` permission, `setup-chainctl` gets OIDC credentials
5. On subsequent pushes to the same PR, approval is required again

### Why `pull_request` + environment works

Normally, `pull_request` from forks runs with read-only permissions and no OIDC tokens.
When an environment with protection rules is configured, GitHub elevates permissions
*after* the required reviewers approve. The environment is the trust gate.

### Changes required

**1. GitHub Environment (via Terraform in chainguard-dev/infra)**

Create `acceptance-tests` environment on `terraform-provider-chainguard` repo with:
- Required reviewer: `customer-platform-team-write` team
- No branch restrictions (needed for PR branches)

**2. Workflow change (this repo)**

```yaml
# Change trigger from pull_request_target to pull_request
on:
  pull_request:
    branches: [main]
  push:
    branches: [main, "releases/**"]

jobs:
  test:
    environment: acceptance-tests  # <-- requires reviewer approval
    # ... rest unchanged
```

### Security comparison

| | `pull_request_target` (old) | `pull_request` + environment (new) |
|---|---|---|
| Fork PR checkout | Ambiguous (base or PR ref) | Always PR code |
| Credentials available | Always | Only after reviewer approval |
| Code reviewed before credentials | No | Yes |
| Maintainer action needed | None | One click per PR |

### Tradeoffs

- Requires one manual approval click per PR for acceptance tests
- Internal PRs from trusted contributors still need approval (could be automated later via GitHub App)
- The test org has limited blast radius regardless of approach
