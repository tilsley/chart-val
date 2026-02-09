# E2E Test Quick Start

Run end-to-end tests with real GitHub PRs in under 2 minutes!

## Quick Run

```bash
# 1. Place GitHub App private key at ./chart-val.pem (if not already)
# 2. Set your GitHub token
export GITHUB_TOKEN="ghp_your_token_here"

# 3. Run E2E tests (uses config from Makefile + .env)
make test-e2e-local
```

**That's it!** The Makefile automatically loads:
- `GITHUB_APP_ID` from .env (required - set your real value)
- `GITHUB_PRIVATE_KEY` from chart-val.pem or .env
- `GITHUB_INSTALLATION_ID` from .env (required - set your real value)
- `WEBHOOK_SECRET` from .env (required - set your real value)

Only `GITHUB_TOKEN` needs to be exported manually.

## What Happens

```
┌─────────────────────────────────────────────────┐
│  E2E Test Workflow                              │
├─────────────────────────────────────────────────┤
│  1. Starts chart-val test server            │
│     (httptest.Server - standard Go pattern)     │
│  2. Creates test branch: e2e-test-1738962000   │
│  3. Adds test chart files                       │
│  4. Opens draft PR #123                         │
│  5. Fetches PR details via GitHub API          │
│  6. Sends webhook to test server               │
│     (simulates GitHub webhook)                  │
│  7. Waits for check run to appear...           │
│  8. ✅ Verifies check run (in-progress→success)│
│  9. ✅ Verifies PR comment posted               │
│  10. Closes PR and deletes branch               │
└─────────────────────────────────────────────────┘
```

**Key Point:** Uses httptest.Server to test the complete webhook flow - this is the idiomatic Go pattern for E2E HTTP testing!

## Expected Output

```
=== RUN   TestE2E_FullWorkflow
    e2e_test.go:29: Running E2E test against tilsley/chart-val
    e2e_test.go:33: Creating test branch: e2e-test-1738962000
    e2e_test.go:41: Adding test chart changes to branch
    e2e_test.go:46: Creating draft PR
    e2e_test.go:51: Created PR #123
    e2e_test.go:54: Waiting for check runs to complete...
    e2e_test.go:62: Verifying check runs...
    e2e_test.go:69: Found check run: chart-val: e2e-test-chart (status: completed, conclusion: success)
    e2e_test.go:85: Check run summary: Analyzed 1 environment(s): 0 changed, 1 unchanged
    e2e_test.go:90: Verifying PR comments...
    e2e_test.go:100: Found chart-val comment (length: 542 chars)
    e2e_test.go:114: ✅ E2E test completed successfully
--- PASS: TestE2E_FullWorkflow (45.23s)
PASS
```

## Troubleshooting

### "Skipping E2E test"
```bash
export E2E_TEST=true
```

### "GITHUB_TOKEN environment variable required"
```bash
# Create token at: https://github.com/settings/tokens
# Needs: repo scope
export GITHUB_TOKEN="ghp_..."
```

### "GITHUB_APP_ID environment variable required"
```bash
export GITHUB_APP_ID="your_app_id"
export GITHUB_PRIVATE_KEY="$(cat chart-val.pem)"
```

### "timeout waiting for check runs"
- Check for errors in test output
- Verify Helm is installed: `helm version`
- Verify GitHub App installation on repo
- Increase timeout in test if needed

### Check runs appear but test fails
- Look at check run details in GitHub UI
- Check test output for specific assertion failures
- Verify PR comment was actually posted

## Advanced Usage

### Test Against Different Repo
```bash
export E2E_OWNER="myorg"
export E2E_REPO="my-helm-repo"
export E2E_BASE_BRANCH="develop"
make test-e2e
```

### Why httptest.Server?

The E2E test uses Go's `httptest.Server` pattern - this is the standard way to test HTTP handlers in Go. Benefits:

- ✅ Tests the complete webhook flow (signature validation, parsing, processing)
- ✅ No external dependencies or port conflicts
- ✅ Fast and deterministic
- ✅ Runs the actual production code (webhook handler + service)

This is NOT "manually invoking" - it's the idiomatic Go way to test HTTP services!

### Run Specific Tests

Run all E2E tests:
```bash
make test-e2e-local
```

Run a specific test:
```bash
export GITHUB_TOKEN="ghp_..."
E2E_TEST=true go test ./test/e2e -v -run TestE2E_FullWorkflow
E2E_TEST=true go test ./test/e2e -v -run TestE2E_HelmTemplateFailure
E2E_TEST=true go test ./test/e2e -v -run TestE2E_NoChartChanges
E2E_TEST=true go test ./test/e2e -v -run TestE2E_ChartWithEnvironments
E2E_TEST=true go test ./test/e2e -v -run TestE2E_UpdateExistingChart
```

### Keep PR Open for Inspection
Comment out the `defer closePR(...)` line in the test to keep the PR open.

## CI/CD Integration

See `README.md` for GitHub Actions workflow example.
