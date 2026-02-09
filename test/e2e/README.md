# End-to-End Tests

These tests create real GitHub PRs and verify the complete chart-val workflow.

## Prerequisites

1. **GitHub Token**: Create a personal access token with `repo` scope
2. **GitHub App Credentials**: Same credentials used to run chart-val
   - `GITHUB_APP_ID`
   - `GITHUB_PRIVATE_KEY`
3. **Helm**: Installed and available on PATH
4. **Test Repository**: Access to a repository where PRs can be created

**Note:** The test uses Go's `httptest.Server` to run the chart-val webhook handler - this is the idiomatic Go pattern for E2E HTTP testing!

## Running E2E Tests

### 1. Set Environment Variables

```bash
# Required
export GITHUB_TOKEN="ghp_your_token_here"
export E2E_TEST=true
export GITHUB_APP_ID="your_app_id"
export GITHUB_PRIVATE_KEY="$(cat /path/to/your/private-key.pem)"

# Optional: customize test repository
export E2E_OWNER="your-github-username"  # default: derived from GITHUB_TOKEN
export E2E_REPO="your-test-repo"         # default: chart-val
export E2E_BASE_BRANCH="main"            # default: main
export E2E_INSTALLATION_ID="your-installation-id"  # from your GitHub App
```

### 2. Run E2E Tests

```bash
go test ./test/e2e -v
```

Or use the Makefile:
```bash
make test-e2e
```

Or with timeout:
```bash
go test ./test/e2e -v -timeout 5m
```

## What the Test Does

1. ✅ Starts chart-val test server (httptest.Server with webhook handler)
2. ✅ Creates a test branch from `main`
3. ✅ Adds a test chart with Chart.yaml and templates
4. ✅ Opens a draft PR
5. ✅ Gets PR details from GitHub API
6. ✅ Sends webhook to test server (simulates GitHub webhook with proper signature)
7. ✅ Waits for check runs to complete (polls every 5s)
8. ✅ Verifies check runs are created with correct status
9. ✅ Verifies PR comment is posted with correct content
10. ✅ Cleans up (closes PR and deletes branch)

**Why httptest.Server?** This is the standard Go pattern for E2E testing of HTTP services. It tests the complete webhook flow (signature validation, parsing, async processing) using the actual production code.

## Makefile Targets

The `make test-e2e` target is already configured:

```makefile
.PHONY: test-e2e
test-e2e:
	@echo "Running E2E tests..."
	E2E_TEST=true go test ./test/e2e -v -timeout 5m
```

No need to start a separate server - the test uses httptest.Server internally!

## Troubleshooting

### Test Skips
- Ensure `E2E_TEST=true` is set
- Ensure `GITHUB_TOKEN` is set

### Check Runs Not Appearing
- Check test output for webhook processing errors
- Ensure GitHub App is installed on the test repository
- Verify GITHUB_APP_ID and GITHUB_PRIVATE_KEY are correct

### Timeout Errors
- Increase timeout: `go test ./test/e2e -v -timeout 10m`
- Check server logs for processing errors
- Verify chart rendering works (helm installed)

## CI/CD Integration

For GitHub Actions:

```yaml
name: E2E Tests
on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.24'

      - name: Install Helm
        run: |
          curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

      - name: Run E2E tests
        run: make test-e2e
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GITHUB_APP_ID: ${{ secrets.GITHUB_APP_ID }}
          GITHUB_PRIVATE_KEY: ${{ secrets.GITHUB_PRIVATE_KEY }}
          E2E_TEST: true
```

## Test Coverage

The E2E tests verify:

### TestE2E_FullWorkflow (basic happy path)
- ✅ Branch creation and cleanup
- ✅ File commits
- ✅ Draft PR creation
- ✅ Check run creation (in-progress → completed)
- ✅ Check run status and conclusion (success)
- ✅ Check run output/summary
- ✅ PR comment creation and content
- ✅ Cleanup (PR close, branch delete)

### TestE2E_HelmTemplateFailure (error handling)
- ✅ Invalid chart YAML detection
- ✅ Helm template failure handling
- ✅ Check run shows "failure" conclusion
- ✅ Error message in check run output

### TestE2E_NoChartChanges (skip non-chart PRs)
- ✅ PR with only non-chart changes (README, src/, etc.)
- ✅ No check runs created
- ✅ No PR comments posted
- ✅ Early return/skip behavior

### TestE2E_ChartWithEnvironments (multi-env discovery)
- ✅ Chart with env/dev-values.yaml
- ✅ Chart with env/staging-values.yaml
- ✅ Chart with env/prod-values.yaml
- ✅ Environment discovery from env/ directory
- ✅ Check run includes all environments
- ✅ PR comment shows all environments
- ✅ Collapsible sections per environment

### TestE2E_UpdateExistingChart (actual diff scenario)
- ✅ Creates base branch with initial chart
- ✅ Creates feature branch from base branch (not from main)
- ✅ Updates chart values (replica counts, image tags)
- ✅ PR from feature to base shows actual diffs
- ✅ Verifies diff shows changes (not "all new")
- ✅ Tests realistic update workflow
- ✅ Both additions and deletions in diff output
