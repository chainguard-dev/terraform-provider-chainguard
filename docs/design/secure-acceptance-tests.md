# Secure Acceptance Tests on Pull Requests

## Problem

The Terraform provider acceptance tests require OIDC credentials (via `setup-chainctl`)
to authenticate against the Chainguard API. The workflow uses `pull_request_target` which
checks out the base branch by default, meaning tests don't run against PR code.

## Solution: `pull_request_target` + Environment Protection + PR Checkout

Use `pull_request_target` (required for OIDC tokens on fork PRs) with a GitHub Environment
that requires reviewer approval, and explicitly checkout the PR code.

### Why not `pull_request` + environment?

Fork PRs **cannot** receive `id-token: write` permission with `pull_request` events,
even with environment approval. This is a GitHub security restriction — fork PRs never
mint OIDC tokens for the target repo. Since this is a public repo where contributors
submit PRs from forks, `pull_request` doesn't work.

### How it works

1. PR is opened → workflow triggers with `pull_request_target` event
2. GitHub shows the job as "Waiting for review" (environment protection)
3. A required reviewer (customer-platform-team-write) clicks "Approve and deploy"
4. The job runs with `id-token: write` in the base repo context
5. The checkout step uses `ref: ${{ github.event.pull_request.head.sha }}` to get PR code
6. Tests run against the actual PR code with OIDC credentials

### Changes required

**1. GitHub Environment (via Terraform in chainguard-dev/infra)**

Create `acceptance-tests` environment on `terraform-provider-chainguard` repo with:
- Required reviewer: `customer-platform-team-write` team
- No branch restrictions (needed for PR branches)

**2. Workflow change (this repo)**

```yaml
on:
  pull_request_target:  # keeps OIDC tokens available for fork PRs
    branches: [main]
  push:
    branches: [main, "releases/**"]

jobs:
  test:
    # Require approval on PRs; push events skip the environment gate
    environment: ${{ github.event_name == 'pull_request_target' && 'acceptance-tests' || '' }}
    steps:
      - uses: actions/checkout@v6
        with:
          # Checkout the PR code, not the base branch
          ref: ${{ github.event.pull_request.head.sha || github.sha }}
```

### Security comparison

| | Before (no environment) | After (with environment) |
|---|---|---|
| Trigger | `pull_request_target` | `pull_request_target` |
| Checkout | Base branch (main) | PR code (`head.sha`) |
| Credentials | Always available | Only after reviewer approval |
| Code reviewed before credentials | No | Yes |
| Maintainer action needed | None | One click per PR |

### Tradeoffs

- Requires one manual approval click per PR for acceptance tests
- After approval, untrusted PR code runs with OIDC credentials (blast radius limited to test org)
- Push events to main/releases run without approval since code is already merged
