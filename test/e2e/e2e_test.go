package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"

	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/nathantilsley/chart-val/internal/platform/telemetry"

	dyffdiff "github.com/nathantilsley/chart-val/internal/diff/adapters/dyff_diff"
	fsenv "github.com/nathantilsley/chart-val/internal/diff/adapters/environment_config/filesystem"
	githubin "github.com/nathantilsley/chart-val/internal/diff/adapters/github_in"
	githubout "github.com/nathantilsley/chart-val/internal/diff/adapters/github_out"
	helmcli "github.com/nathantilsley/chart-val/internal/diff/adapters/helm_cli"
	linediff "github.com/nathantilsley/chart-val/internal/diff/adapters/line_diff"
	prfiles "github.com/nathantilsley/chart-val/internal/diff/adapters/pr_files"
	sourcectrl "github.com/nathantilsley/chart-val/internal/diff/adapters/source_ctrl"
	"github.com/nathantilsley/chart-val/internal/diff/app"
	ghclient "github.com/nathantilsley/chart-val/internal/platform/github"
	"github.com/nathantilsley/chart-val/internal/platform/logger"
)

const (
	e2eTestEnvValue    = "true"
	checkRunName       = "chart-val"
	checkRunStatusDone = "completed"
)

// TestE2E_FullWorkflow creates a real PR, triggers diff, and verifies results.
// Requires: GITHUB_TOKEN and E2E_TEST=true environment variables.
func TestE2E_FullWorkflow(t *testing.T) {
	if os.Getenv("E2E_TEST") != e2eTestEnvValue {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Fatal("GITHUB_TOKEN environment variable required for E2E tests")
	}

	// Test configuration
	owner := getEnvOrDefault("E2E_OWNER", "tilsley")
	repo := getEnvOrDefault("E2E_REPO", "chart-val")
	baseBranch := getEnvOrDefault("E2E_BASE_BRANCH", "main")
	webhookSecret := getEnvOrDefault("WEBHOOK_SECRET", "test")

	// Get GitHub App credentials
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		t.Fatal("GITHUB_APP_ID environment variable required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("invalid GITHUB_APP_ID: %v", err)
	}

	privateKey := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKey == "" {
		t.Fatal("GITHUB_PRIVATE_KEY environment variable required")
	}

	installationID := getEnvOrDefaultInt64("E2E_INSTALLATION_ID", getEnvOrDefaultInt64("GITHUB_INSTALLATION_ID", 0))
	if installationID == 0 {
		t.Fatal("GITHUB_INSTALLATION_ID or E2E_INSTALLATION_ID environment variable required")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	t.Logf("Running E2E test against %s/%s", owner, repo)

	// Clean up any orphaned e2e-test branches from previous failed runs
	t.Logf("Cleaning up orphaned e2e-test branches...")
	cleanupOrphanedBranches(ctx, client, owner, repo, t)

	// Set up chart-val test server
	t.Logf("Starting chart-val test server...")
	testServer := setupTestServer(t, appID, installationID, privateKey, webhookSecret)
	defer testServer.Close()
	t.Logf("Test server running at %s", testServer.URL)

	// Step 1: Create a test branch with chart changes
	// Use UnixNano for better uniqueness (prevents collisions if tests run in same second)
	testBranch := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	t.Logf("Creating test branch: %s", testBranch)

	if err := createTestBranch(ctx, client, owner, repo, baseBranch, testBranch); err != nil {
		t.Fatalf("failed to create test branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, testBranch, t)

	// Step 2: Add test chart changes to the branch
	t.Logf("Adding test chart changes to branch")
	if err := addTestChartChanges(ctx, client, owner, repo, testBranch); err != nil {
		t.Fatalf("failed to add test changes: %v", err)
	}

	// Step 3: Create a draft PR
	t.Logf("Creating draft PR")
	prNumber, err := createDraftPR(ctx, client, owner, repo, baseBranch, testBranch, "E2E Test: Chart Changes")
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}
	defer closePR(ctx, client, owner, repo, prNumber, t)

	t.Logf("Created PR #%d", prNumber)

	// Step 4: Get PR details
	t.Logf("Fetching PR details...")
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR details: %v", err)
	}

	// Wait for GitHub to process the PR and compute the file diff
	t.Logf("Waiting for GitHub to process PR...")
	time.Sleep(5 * time.Second)

	// Step 5: Send webhook to test server
	t.Logf("Sending webhook to test server...")
	webhookURL := testServer.URL + "/webhook"
	if err := sendWebhook(ctx, webhookURL, webhookSecret, owner, repo, pr, installationID); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	// Step 6: Wait for check runs to complete
	t.Logf("Waiting for check runs to complete...")
	checkRuns, err := waitForCheckRuns(ctx, client, owner, repo, testBranch, 2*time.Minute)
	if err != nil {
		t.Fatalf("failed waiting for check runs: %v", err)
	}

	// Step 7: Verify check runs
	t.Logf("Verifying check runs...")
	foundChartSentry := false
	for _, run := range checkRuns {
		if run.GetName() == checkRunName {
			foundChartSentry = true
			t.Logf("Found check run: %s (status: %s, conclusion: %s)",
				run.GetName(), run.GetStatus(), run.GetConclusion())

			// Verify status is completed
			if run.GetStatus() != checkRunStatusDone {
				t.Errorf("Expected status 'completed', got '%s'", run.GetStatus())
			}

			// Verify conclusion is success (green check)
			if run.GetConclusion() != "success" && run.GetConclusion() != "failure" {
				t.Errorf("Expected conclusion 'success' or 'failure', got '%s'", run.GetConclusion())
			}

			// Verify output exists
			if run.Output == nil {
				t.Error("Expected check run output, got nil")
			} else {
				t.Logf("Check run summary: %s", run.Output.GetSummary())
			}
		}
	}

	if !foundChartSentry {
		t.Error("No chart-val check runs found")
	}

	// Step 8: Verify PR comments
	t.Logf("Verifying PR comments...")
	comments, err := getPRComments(ctx, client, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR comments: %v", err)
	}

	foundComment := false
	for _, comment := range comments {
		body := comment.GetBody()
		if strings.Contains(body, "Helm Diff Report") {
			foundComment = true
			t.Logf("Found chart-val comment (length: %d chars)", len(body))

			// Verify comment contains expected elements
			if !strings.Contains(body, "Status:") {
				t.Error("Comment missing 'Status:' section")
			}
			if !strings.Contains(body, "Environment") {
				t.Error("Comment missing 'Environment' table")
			}
		}
	}

	if !foundComment {
		t.Error("No chart-val PR comment found")
	}

	t.Logf("✅ E2E test completed successfully")
}

// TestE2E_HelmTemplateFailure tests the scenario where helm template fails.
func TestE2E_HelmTemplateFailure(t *testing.T) {
	if os.Getenv("E2E_TEST") != e2eTestEnvValue {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Fatal("GITHUB_TOKEN environment variable required for E2E tests")
	}

	// Test configuration
	owner := getEnvOrDefault("E2E_OWNER", "tilsley")
	repo := getEnvOrDefault("E2E_REPO", "chart-val")
	baseBranch := getEnvOrDefault("E2E_BASE_BRANCH", "main")
	webhookSecret := getEnvOrDefault("WEBHOOK_SECRET", "test")

	// Get GitHub App credentials
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		t.Fatal("GITHUB_APP_ID environment variable required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("invalid GITHUB_APP_ID: %v", err)
	}

	privateKey := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKey == "" {
		t.Fatal("GITHUB_PRIVATE_KEY environment variable required")
	}

	installationID := getEnvOrDefaultInt64("E2E_INSTALLATION_ID", getEnvOrDefaultInt64("GITHUB_INSTALLATION_ID", 0))
	if installationID == 0 {
		t.Fatal("GITHUB_INSTALLATION_ID or E2E_INSTALLATION_ID environment variable required")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	t.Logf("Running E2E test (failure scenario) against %s/%s", owner, repo)

	// Clean up orphaned branches
	t.Logf("Cleaning up orphaned e2e-test branches...")
	cleanupOrphanedBranches(ctx, client, owner, repo, t)

	// Set up test server
	t.Logf("Starting chart-val test server...")
	testServer := setupTestServer(t, appID, installationID, privateKey, webhookSecret)
	defer testServer.Close()
	t.Logf("Test server running at %s", testServer.URL)

	// Create test branch with invalid chart
	testBranch := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	t.Logf("Creating test branch: %s", testBranch)

	if err := createTestBranch(ctx, client, owner, repo, baseBranch, testBranch); err != nil {
		t.Fatalf("failed to create test branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, testBranch, t)

	// Add invalid chart (will cause helm template to fail)
	t.Logf("Adding invalid chart to branch")
	if err := addInvalidChart(ctx, client, owner, repo, testBranch); err != nil {
		t.Fatalf("failed to add invalid chart: %v", err)
	}

	// Create draft PR
	t.Logf("Creating draft PR")
	prNumber, err := createDraftPR(ctx, client, owner, repo, baseBranch, testBranch, "E2E Test: Invalid Chart")
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}
	defer closePR(ctx, client, owner, repo, prNumber, t)

	t.Logf("Created PR #%d", prNumber)

	// Get PR details
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR details: %v", err)
	}

	// Wait for GitHub to process the PR and compute the file diff
	t.Logf("Waiting for GitHub to process PR...")
	time.Sleep(5 * time.Second)

	// Send webhook
	t.Logf("Sending webhook to test server...")
	webhookURL := testServer.URL + "/webhook"
	if err := sendWebhook(ctx, webhookURL, webhookSecret, owner, repo, pr, installationID); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	// Wait for check runs
	t.Logf("Waiting for check runs to complete...")
	checkRuns, err := waitForCheckRuns(ctx, client, owner, repo, testBranch, 2*time.Minute)
	if err != nil {
		t.Fatalf("failed waiting for check runs: %v", err)
	}

	// Verify check run shows failure
	t.Logf("Verifying check runs show failure...")
	foundFailure := false
	for _, run := range checkRuns {
		if run.GetName() == checkRunName {
			t.Logf("Found check run: %s (status: %s, conclusion: %s)",
				run.GetName(), run.GetStatus(), run.GetConclusion())

			if run.GetStatus() == "completed" && run.GetConclusion() == "failure" {
				foundFailure = true
				t.Logf("✓ Check run correctly shows failure")
			}
		}
	}

	if !foundFailure {
		t.Error("Expected to find check run with 'failure' conclusion")
	}

	t.Logf("✅ E2E failure test completed successfully")
}

// TestE2E_NoChartChanges tests the scenario where PR doesn't modify any charts.
func TestE2E_NoChartChanges(t *testing.T) {
	if os.Getenv("E2E_TEST") != e2eTestEnvValue {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Fatal("GITHUB_TOKEN environment variable required for E2E tests")
	}

	// Test configuration
	owner := getEnvOrDefault("E2E_OWNER", "tilsley")
	repo := getEnvOrDefault("E2E_REPO", "chart-val")
	baseBranch := getEnvOrDefault("E2E_BASE_BRANCH", "main")
	webhookSecret := getEnvOrDefault("WEBHOOK_SECRET", "test")

	// Get GitHub App credentials
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		t.Fatal("GITHUB_APP_ID environment variable required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("invalid GITHUB_APP_ID: %v", err)
	}

	privateKey := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKey == "" {
		t.Fatal("GITHUB_PRIVATE_KEY environment variable required")
	}

	installationID := getEnvOrDefaultInt64("E2E_INSTALLATION_ID", getEnvOrDefaultInt64("GITHUB_INSTALLATION_ID", 0))
	if installationID == 0 {
		t.Fatal("GITHUB_INSTALLATION_ID or E2E_INSTALLATION_ID environment variable required")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	t.Logf("Running E2E test (no chart changes) against %s/%s", owner, repo)

	// Clean up orphaned branches
	cleanupOrphanedBranches(ctx, client, owner, repo, t)

	// Set up test server
	testServer := setupTestServer(t, appID, installationID, privateKey, webhookSecret)
	defer testServer.Close()

	// Create test branch with non-chart changes
	testBranch := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	t.Logf("Creating test branch: %s", testBranch)

	if err := createTestBranch(ctx, client, owner, repo, baseBranch, testBranch); err != nil {
		t.Fatalf("failed to create test branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, testBranch, t)

	// Add non-chart changes (README update)
	t.Logf("Adding non-chart changes to branch")
	if err := addNonChartChanges(ctx, client, owner, repo, testBranch); err != nil {
		t.Fatalf("failed to add non-chart changes: %v", err)
	}

	// Create draft PR
	t.Logf("Creating draft PR")
	prNumber, err := createDraftPR(ctx, client, owner, repo, baseBranch, testBranch, "E2E Test: No Chart Changes")
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}
	defer closePR(ctx, client, owner, repo, prNumber, t)

	t.Logf("Created PR #%d", prNumber)

	// Get PR details
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR details: %v", err)
	}

	// Wait for GitHub to process the PR and compute the file diff
	t.Logf("Waiting for GitHub to process PR...")
	time.Sleep(5 * time.Second)

	// Send webhook
	t.Logf("Sending webhook to test server...")
	webhookURL := testServer.URL + "/webhook"
	if err := sendWebhook(ctx, webhookURL, webhookSecret, owner, repo, pr, installationID); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	// Wait a bit to ensure no check runs are created
	time.Sleep(10 * time.Second)

	// Verify NO check runs were created
	t.Logf("Verifying no check runs were created...")
	checkRuns, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, testBranch, &github.ListCheckRunsOptions{})
	if err != nil {
		t.Fatalf("failed to list check runs: %v", err)
	}

	foundChartSentry := false
	for _, run := range checkRuns.CheckRuns {
		if run.GetName() == checkRunName {
			foundChartSentry = true
			t.Errorf("Unexpected check run found: %s", run.GetName())
		}
	}

	if foundChartSentry {
		t.Error("Expected no chart-val check runs for non-chart changes")
	} else {
		t.Logf("✓ Correctly skipped check runs for non-chart changes")
	}

	t.Logf("✅ E2E no-chart-changes test completed successfully")
}

// TestE2E_ChartWithEnvironments tests multi-environment discovery.
func TestE2E_ChartWithEnvironments(t *testing.T) {
	if os.Getenv("E2E_TEST") != e2eTestEnvValue {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Fatal("GITHUB_TOKEN environment variable required for E2E tests")
	}

	// Test configuration
	owner := getEnvOrDefault("E2E_OWNER", "tilsley")
	repo := getEnvOrDefault("E2E_REPO", "chart-val")
	baseBranch := getEnvOrDefault("E2E_BASE_BRANCH", "main")
	webhookSecret := getEnvOrDefault("WEBHOOK_SECRET", "test")

	// Get GitHub App credentials
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		t.Fatal("GITHUB_APP_ID environment variable required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("invalid GITHUB_APP_ID: %v", err)
	}

	privateKey := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKey == "" {
		t.Fatal("GITHUB_PRIVATE_KEY environment variable required")
	}

	installationID := getEnvOrDefaultInt64("E2E_INSTALLATION_ID", getEnvOrDefaultInt64("GITHUB_INSTALLATION_ID", 0))
	if installationID == 0 {
		t.Fatal("GITHUB_INSTALLATION_ID or E2E_INSTALLATION_ID environment variable required")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	t.Logf("Running E2E test (multi-environment) against %s/%s", owner, repo)

	// Clean up orphaned branches
	cleanupOrphanedBranches(ctx, client, owner, repo, t)

	// Set up test server
	testServer := setupTestServer(t, appID, installationID, privateKey, webhookSecret)
	defer testServer.Close()

	// Create test branch
	testBranch := fmt.Sprintf("e2e-test-%d", time.Now().UnixNano())
	t.Logf("Creating test branch: %s", testBranch)

	if err := createTestBranch(ctx, client, owner, repo, baseBranch, testBranch); err != nil {
		t.Fatalf("failed to create test branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, testBranch, t)

	// Add chart with multiple environments
	t.Logf("Adding chart with dev, staging, and prod environments")
	if err := addChartWithEnvironments(ctx, client, owner, repo, testBranch); err != nil {
		t.Fatalf("failed to add chart with environments: %v", err)
	}

	// Create draft PR
	t.Logf("Creating draft PR")
	prNumber, err := createDraftPR(
		ctx,
		client,
		owner,
		repo,
		baseBranch,
		testBranch,
		"E2E Test: Multi-Environment Chart",
	)
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}
	defer closePR(ctx, client, owner, repo, prNumber, t)

	t.Logf("Created PR #%d", prNumber)

	// Get PR details
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR details: %v", err)
	}

	// Wait for GitHub to process the PR and compute the file diff
	t.Logf("Waiting for GitHub to process PR...")
	time.Sleep(5 * time.Second)

	// Send webhook
	t.Logf("Sending webhook to test server...")
	webhookURL := testServer.URL + "/webhook"
	if err := sendWebhook(ctx, webhookURL, webhookSecret, owner, repo, pr, installationID); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	// Wait for check runs
	t.Logf("Waiting for check runs to complete...")
	checkRuns, err := waitForCheckRuns(ctx, client, owner, repo, testBranch, 2*time.Minute)
	if err != nil {
		t.Fatalf("failed waiting for check runs: %v", err)
	}

	// Verify check run includes all environments
	t.Logf("Verifying check runs include all environments...")
	foundChartSentry := false
	for _, run := range checkRuns {
		if run.GetName() == checkRunName {
			foundChartSentry = true
			t.Logf("Found check run: %s", run.GetName())

			if run.Output != nil {
				summary := run.Output.GetSummary()
				text := run.Output.GetText()

				// Verify summary shows analyzed charts
				if !strings.Contains(summary, "Analyzed") {
					t.Error("Expected summary to show analyzed charts")
				}

				// Verify text includes dev, staging, prod sections
				for _, env := range []string{"dev", "staging", "prod"} {
					if !strings.Contains(text, env) {
						t.Errorf("Expected check run text to include environment: %s", env)
					}
				}

				t.Logf("✓ Check run includes all environments (dev, staging, prod)")
			}
		}
	}

	if !foundChartSentry {
		t.Error("No chart-val check runs found")
	}

	// Verify PR comment mentions all environments
	comments, err := getPRComments(ctx, client, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR comments: %v", err)
	}

	foundComment := false
	for _, comment := range comments {
		body := comment.GetBody()
		if strings.Contains(body, "Helm Diff Report") {
			foundComment = true

			for _, env := range []string{"dev", "staging", "prod"} {
				if !strings.Contains(body, env) {
					t.Errorf("Expected PR comment to include environment: %s", env)
				}
			}

			t.Logf("✓ PR comment includes all environments")
		}
	}

	if !foundComment {
		t.Error("No chart-val PR comment found")
	}

	t.Logf("✅ E2E multi-environment test completed successfully")
}

// TestE2E_UpdateExistingChart tests updating an existing chart (not adding new).
// Creates base branch with chart, then feature branch that modifies it.
func TestE2E_UpdateExistingChart(t *testing.T) {
	if os.Getenv("E2E_TEST") != e2eTestEnvValue {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Fatal("GITHUB_TOKEN environment variable required for E2E tests")
	}

	// Test configuration
	owner := getEnvOrDefault("E2E_OWNER", "tilsley")
	repo := getEnvOrDefault("E2E_REPO", "chart-val")
	baseBranch := getEnvOrDefault("E2E_BASE_BRANCH", "main")
	webhookSecret := getEnvOrDefault("WEBHOOK_SECRET", "test")

	// Get GitHub App credentials
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		t.Fatal("GITHUB_APP_ID environment variable required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		t.Fatalf("invalid GITHUB_APP_ID: %v", err)
	}

	privateKey := os.Getenv("GITHUB_PRIVATE_KEY")
	if privateKey == "" {
		t.Fatal("GITHUB_PRIVATE_KEY environment variable required")
	}

	installationID := getEnvOrDefaultInt64("E2E_INSTALLATION_ID", getEnvOrDefaultInt64("GITHUB_INSTALLATION_ID", 0))
	if installationID == 0 {
		t.Fatal("GITHUB_INSTALLATION_ID or E2E_INSTALLATION_ID environment variable required")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	t.Logf("Running E2E test (update existing chart) against %s/%s", owner, repo)

	// Clean up orphaned branches
	cleanupOrphanedBranches(ctx, client, owner, repo, t)

	// Set up test server
	testServer := setupTestServer(t, appID, installationID, privateKey, webhookSecret)
	defer testServer.Close()

	// Step 1: Create base branch with initial chart
	baseBranchName := fmt.Sprintf("e2e-test-%d-base", time.Now().UnixNano())
	t.Logf("Creating base branch with initial chart: %s", baseBranchName)

	if err := createTestBranch(ctx, client, owner, repo, baseBranch, baseBranchName); err != nil {
		t.Fatalf("failed to create base branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, baseBranchName, t)

	// Add chart with multiple environments to base branch
	if err := addChartWithEnvironments(ctx, client, owner, repo, baseBranchName); err != nil {
		t.Fatalf("failed to add chart to base branch: %v", err)
	}

	// Wait a moment for GitHub to process the commits
	time.Sleep(2 * time.Second)

	// Step 2: Create feature branch from base branch (not from main)
	featureBranchName := fmt.Sprintf("e2e-test-%d-feature", time.Now().UnixNano())
	t.Logf("Creating feature branch from base branch: %s", featureBranchName)

	// Get base branch ref to create feature branch from it
	baseRef, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranchName)
	if err != nil {
		t.Fatalf("failed to get base branch ref: %v", err)
	}

	featureRef := &github.Reference{
		Ref: github.Ptr("refs/heads/" + featureBranchName),
		Object: &github.GitObject{
			SHA: baseRef.Object.SHA,
		},
	}

	_, _, err = client.Git.CreateRef(ctx, owner, repo, featureRef)
	if err != nil {
		t.Fatalf("failed to create feature branch: %v", err)
	}
	defer cleanupBranch(ctx, client, owner, repo, featureBranchName, t)

	// Step 3: Update chart values in feature branch
	t.Logf("Updating chart values in feature branch")
	if err := updateChartValues(ctx, client, owner, repo, featureBranchName); err != nil {
		t.Fatalf("failed to update chart values: %v", err)
	}

	// Step 4: Create PR from feature branch to base branch (not to main!)
	t.Logf("Creating PR from %s to %s", featureBranchName, baseBranchName)
	prNumber, err := createDraftPR(
		ctx,
		client,
		owner,
		repo,
		baseBranchName,
		featureBranchName,
		"E2E Test: Update Chart Values",
	)
	if err != nil {
		t.Fatalf("failed to create PR: %v", err)
	}
	defer closePR(ctx, client, owner, repo, prNumber, t)

	t.Logf("Created PR #%d", prNumber)

	// Step 5: Get PR details
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR details: %v", err)
	}

	// Wait for GitHub to process the PR and compute the file diff
	// This is especially important when comparing non-main branches
	t.Logf("Waiting for GitHub to process PR...")
	time.Sleep(5 * time.Second)

	// Step 6: Send webhook
	t.Logf("Sending webhook to test server...")
	webhookURL := testServer.URL + "/webhook"
	if err := sendWebhook(ctx, webhookURL, webhookSecret, owner, repo, pr, installationID); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	// Step 7: Wait for check runs
	t.Logf("Waiting for check runs to complete...")
	checkRuns, err := waitForCheckRuns(ctx, client, owner, repo, featureBranchName, 2*time.Minute)
	if err != nil {
		t.Fatalf("failed waiting for check runs: %v", err)
	}

	// Step 8: Verify check runs show actual diffs (not "new chart")
	t.Logf("Verifying check runs show actual diffs...")
	foundChartSentry := false
	for _, run := range checkRuns {
		if run.GetName() == checkRunName {
			foundChartSentry = true
			t.Logf("Found check run: %s", run.GetName())

			if run.Output != nil {
				summary := run.Output.GetSummary()
				text := run.Output.GetText()

				t.Logf("Check run summary: %s", summary)

				// Verify it shows changes (charts analyzed and changes detected)
				if !strings.Contains(summary, "Analyzed") || !strings.Contains(summary, "changes") {
					t.Error("Expected summary to show analyzed charts with changes")
				}

				// Verify the diff shows additions
				if !strings.Contains(text, "+") {
					t.Error("Expected diff to show additions (+)")
				}

				// Verify it's an update scenario (not all new)
				if strings.Contains(text, "Changes detected") || strings.Contains(summary, "changes") {
					t.Logf("✓ Check run shows actual diff (update scenario)")
				} else {
					t.Error("Expected check run to show changes (update scenario), got something else")
				}

				// Verify diff includes environment details in the text body
				for _, env := range []string{"dev", "staging", "prod"} {
					if !strings.Contains(text, env) {
						t.Errorf("Expected check run text to include environment: %s", env)
					}
				}
			}
		}
	}

	if !foundChartSentry {
		t.Error("No chart-val check runs found")
	}

	// Step 9: Verify PR comment
	comments, err := getPRComments(ctx, client, owner, repo, prNumber)
	if err != nil {
		t.Fatalf("failed to get PR comments: %v", err)
	}

	foundComment := false
	for _, comment := range comments {
		body := comment.GetBody()
		if strings.Contains(body, "Helm Diff Report") {
			foundComment = true
			t.Logf("✓ PR comment posted")

			// Verify comment shows changes
			if !strings.Contains(body, "Changed") && !strings.Contains(body, "changes") {
				t.Error("Expected PR comment to indicate changes")
			}
		}
	}

	if !foundComment {
		t.Error("No chart-val PR comment found")
	}

	t.Logf("✅ E2E update-existing-chart test completed successfully")
}

// Helper functions

func readFixture(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join("fixtures", path))
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvOrDefaultInt64(key string, defaultValue int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

func setupTestServer(t *testing.T, appID, installationID int64, privateKey, webhookSecret string) *httptest.Server {
	t.Helper()

	// Create logger
	log := logger.New("debug") // Changed to debug to see more details

	// Create GitHub client with auto-renewing authentication
	githubClient, err := ghclient.NewClient(appID, installationID, privateKey)
	if err != nil {
		t.Fatalf("creating GitHub client: %v", err)
	}

	// Set up adapters
	sourceCtrl := sourcectrl.New(githubClient)
	helmRenderer, err := helmcli.New()
	if err != nil {
		t.Fatalf("creating helm adapter: %v", err)
	}
	reporter := githubout.New(githubClient)
	changedCharts := prfiles.New(githubClient, log)
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	// Environment config: filesystem discovery
	filesystemEnvConfig := fsenv.New(sourceCtrl)

	// Use real OTel when OTEL_ENABLED=true (e.g., with local Jaeger),
	// otherwise noop for zero overhead in normal test runs.
	meter := noopmetric.NewMeterProvider().Meter("test")
	tracer := nooptrace.NewTracerProvider().Tracer("test")
	if os.Getenv("OTEL_ENABLED") == "true" {
		ctx := context.Background()
		tel, err := telemetry.New(ctx, true)
		if err != nil {
			t.Fatalf("initializing telemetry: %v", err)
		}
		t.Cleanup(func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tel.Shutdown(shutdownCtx); err != nil {
				t.Logf("telemetry shutdown: %v", err)
			}
		})
		meter = tel.Meter
		tracer = tel.Tracer
		t.Logf("OTel enabled — traces will be exported to OTLP endpoint")
	}

	// Create service (no Argo in E2E tests)
	diffService := app.NewDiffService(
		sourceCtrl,
		changedCharts,
		nil,                 // No Argo config in E2E
		filesystemEnvConfig, // Use filesystem discovery
		helmRenderer,
		reporter,
		semanticDiff,
		unifiedDiff,
		log,
		meter,
		tracer,
	)

	// Create webhook handler
	webhookHandler := githubin.NewWebhookHandler(diffService, webhookSecret, log)

	// Create test server with webhook handler
	mux := http.NewServeMux()
	mux.Handle("/webhook", webhookHandler)

	return httptest.NewServer(mux)
}

func sendWebhook(
	ctx context.Context,
	webhookURL, webhookSecret, owner, repo string,
	pr *github.PullRequest,
	installationID int64,
) error {
	// Construct webhook payload
	payload := map[string]interface{}{
		"action": "synchronize",
		"number": pr.GetNumber(),
		"pull_request": map[string]interface{}{
			"number": pr.GetNumber(),
			"base": map[string]interface{}{
				"ref": pr.GetBase().GetRef(),
			},
			"head": map[string]interface{}{
				"ref": pr.GetHead().GetRef(),
				"sha": pr.GetHead().GetSHA(),
			},
		},
		"repository": map[string]interface{}{
			"name": repo,
			"owner": map[string]interface{}{
				"login": owner,
			},
		},
		"installation": map[string]interface{}{
			"id": installationID,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	// Sign the payload
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payloadBytes)
	signature := hex.EncodeToString(mac.Sum(nil))

	// Send webhook request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256="+signature)
	req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("e2e-test-%d", time.Now().Unix()))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func createTestBranch(ctx context.Context, client *github.Client, owner, repo, baseBranch, newBranch string) error {
	// Get base branch ref
	baseRef, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("getting base ref: %w", err)
	}

	// Create new branch pointing to same commit
	newRef := &github.Reference{
		Ref: github.Ptr("refs/heads/" + newBranch),
		Object: &github.GitObject{
			SHA: baseRef.Object.SHA,
		},
	}

	_, _, err = client.Git.CreateRef(ctx, owner, repo, newRef)
	if err != nil {
		// If branch already exists somehow, try to delete and recreate
		if strings.Contains(err.Error(), "Reference already exists") {
			_, delErr := client.Git.DeleteRef(ctx, owner, repo, "refs/heads/"+newBranch)
			if delErr != nil {
				return fmt.Errorf("branch exists and failed to delete: %w", delErr)
			}
			_, _, err = client.Git.CreateRef(ctx, owner, repo, newRef)
		}
	}
	return err
}

func addTestChartChanges(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	// Read Chart.yaml from fixture
	chartContent, err := readFixture("test-chart/Chart.yaml")
	if err != nil {
		return fmt.Errorf("reading chart fixture: %w", err)
	}

	chartPath := "charts/e2e-test-chart/Chart.yaml"
	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr("Add E2E test chart"),
		Content: chartContent,
		Branch:  github.Ptr(branch),
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, chartPath, opts)
	if err != nil {
		// If file exists, try updating it
		existingFile, _, _, _ := client.Repositories.GetContents(
			ctx,
			owner,
			repo,
			chartPath,
			&github.RepositoryContentGetOptions{
				Ref: branch,
			},
		)
		if existingFile != nil {
			opts.SHA = existingFile.SHA
			_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, chartPath, opts)
		}
	}

	if err != nil {
		return fmt.Errorf("creating/updating chart file: %w", err)
	}

	// Read deployment template from fixture
	templateContent, err := readFixture("test-chart/templates/deployment.yaml")
	if err != nil {
		return fmt.Errorf("reading template fixture: %w", err)
	}

	templatePath := "charts/e2e-test-chart/templates/deployment.yaml"
	opts = &github.RepositoryContentFileOptions{
		Message: github.Ptr("Add E2E test template"),
		Content: templateContent,
		Branch:  github.Ptr(branch),
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, templatePath, opts)
	if err != nil {
		existingFile, _, _, _ := client.Repositories.GetContents(
			ctx,
			owner,
			repo,
			templatePath,
			&github.RepositoryContentGetOptions{
				Ref: branch,
			},
		)
		if existingFile != nil {
			opts.SHA = existingFile.SHA
			_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, templatePath, opts)
		}
	}

	return err
}

func createDraftPR(ctx context.Context, client *github.Client, owner, repo, base, head, title string) (int, error) {
	// Check if a PR already exists with this head branch
	existingPRs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State: "all",
		Head:  fmt.Sprintf("%s:%s", owner, head),
		Base:  base,
	})
	if err == nil && len(existingPRs) > 0 {
		// Close any existing PRs with this head branch
		for _, existingPR := range existingPRs {
			state := "closed"
			_, _, _ = client.PullRequests.Edit(ctx, owner, repo, existingPR.GetNumber(), &github.PullRequest{
				State: &state,
			})
		}
	}

	pr := &github.NewPullRequest{
		Title: github.Ptr(title),
		Head:  github.Ptr(head),
		Base:  github.Ptr(base),
		Body:  github.Ptr("Automated E2E test PR - will be closed automatically"),
		Draft: github.Ptr(true),
	}

	created, _, err := client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return 0, err
	}

	return created.GetNumber(), nil
}

func waitForCheckRuns(
	ctx context.Context,
	client *github.Client,
	owner, repo, ref string,
	timeout time.Duration,
) ([]*github.CheckRun, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		result, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, &github.ListCheckRunsOptions{})
		if err != nil {
			return nil, err
		}

		// Check if any chart-val check runs are completed
		completed := false
		for _, run := range result.CheckRuns {
			if run.GetName() == checkRunName && run.GetStatus() == "completed" {
				completed = true
				break
			}
		}

		if completed {
			return result.CheckRuns, nil
		}

		time.Sleep(5 * time.Second)
	}

	return nil, errors.New("timeout waiting for check runs to complete")
}

func getPRComments(
	ctx context.Context,
	client *github.Client,
	owner, repo string,
	prNumber int,
) ([]*github.IssueComment, error) {
	comments, _, err := client.Issues.ListComments(ctx, owner, repo, prNumber, &github.IssueListCommentsOptions{})
	return comments, err
}

func closePR(ctx context.Context, client *github.Client, owner, repo string, prNumber int, t *testing.T) {
	t.Logf("Closing PR #%d", prNumber)
	state := "closed"
	_, _, err := client.PullRequests.Edit(ctx, owner, repo, prNumber, &github.PullRequest{
		State: &state,
	})
	if err != nil {
		t.Logf("Warning: failed to close PR: %v", err)
	}
}

func cleanupBranch(ctx context.Context, client *github.Client, owner, repo, branch string, t *testing.T) {
	t.Logf("Deleting test branch: %s", branch)
	_, err := client.Git.DeleteRef(ctx, owner, repo, "refs/heads/"+branch)
	if err != nil {
		t.Logf("Warning: failed to delete branch: %v", err)
	}
}

func cleanupOrphanedBranches(ctx context.Context, client *github.Client, owner, repo string, t *testing.T) {
	// List all branches
	branches, _, err := client.Repositories.ListBranches(ctx, owner, repo, &github.BranchListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		t.Logf("Warning: failed to list branches for cleanup: %v", err)
		return
	}

	// Delete any branches starting with "e2e-test-"
	for _, branch := range branches {
		branchName := branch.GetName()
		if strings.HasPrefix(branchName, "e2e-test-") {
			t.Logf("Cleaning up orphaned branch: %s", branchName)
			_, err := client.Git.DeleteRef(ctx, owner, repo, "refs/heads/"+branchName)
			if err != nil {
				t.Logf("Warning: failed to delete orphaned branch %s: %v", branchName, err)
			}
		}
	}
}

func addInvalidChart(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	// Read Chart.yaml from fixture
	chartContent, err := readFixture("invalid-chart/Chart.yaml")
	if err != nil {
		return fmt.Errorf("reading chart fixture: %w", err)
	}

	chartPath := "charts/invalid-chart/Chart.yaml"
	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr("Add invalid chart"),
		Content: chartContent,
		Branch:  github.Ptr(branch),
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, chartPath, opts)
	if err != nil {
		return fmt.Errorf("creating chart: %w", err)
	}

	// Read invalid template from fixture
	templateContent, err := readFixture("invalid-chart/templates/deployment.yaml")
	if err != nil {
		return fmt.Errorf("reading template fixture: %w", err)
	}

	templatePath := "charts/invalid-chart/templates/deployment.yaml"
	opts = &github.RepositoryContentFileOptions{
		Message: github.Ptr("Add invalid template"),
		Content: templateContent,
		Branch:  github.Ptr(branch),
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, templatePath, opts)
	return err
}

func addNonChartChanges(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	// Update README
	readmePath := "README.md"
	readmeContent := fmt.Sprintf("# Test Repository\n\nUpdated by E2E test at %s\n", time.Now().Format(time.RFC3339))

	// Get existing README if it exists
	existingFile, _, _, _ := client.Repositories.GetContents(
		ctx,
		owner,
		repo,
		readmePath,
		&github.RepositoryContentGetOptions{
			Ref: branch,
		},
	)

	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr("Update README (non-chart change)"),
		Content: []byte(readmeContent),
		Branch:  github.Ptr(branch),
	}

	if existingFile != nil {
		opts.SHA = existingFile.SHA
		_, _, err := client.Repositories.UpdateFile(ctx, owner, repo, readmePath, opts)
		return err
	}

	_, _, err := client.Repositories.CreateFile(ctx, owner, repo, readmePath, opts)
	return err
}

func addChartWithEnvironments(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	chartName := "multi-env-app"

	// Define all files to create from fixtures
	files := map[string]string{
		fmt.Sprintf("charts/%s/Chart.yaml", chartName):                "multi-env-app/Chart.yaml",
		fmt.Sprintf("charts/%s/values.yaml", chartName):               "multi-env-app/values.yaml",
		fmt.Sprintf("charts/%s/templates/deployment.yaml", chartName): "multi-env-app/templates/deployment.yaml",
		fmt.Sprintf("charts/%s/env/dev-values.yaml", chartName):       "multi-env-app/env/dev-values.yaml",
		fmt.Sprintf("charts/%s/env/staging-values.yaml", chartName):   "multi-env-app/env/staging-values.yaml",
		fmt.Sprintf("charts/%s/env/prod-values.yaml", chartName):      "multi-env-app/env/prod-values.yaml",
	}

	for repoPath, fixturePath := range files {
		content, err := readFixture(fixturePath)
		if err != nil {
			return fmt.Errorf("reading fixture %s: %w", fixturePath, err)
		}

		opts := &github.RepositoryContentFileOptions{
			Message: github.Ptr("Add " + filepath.Base(repoPath)),
			Content: content,
			Branch:  github.Ptr(branch),
		}

		if _, _, err := client.Repositories.CreateFile(ctx, owner, repo, repoPath, opts); err != nil {
			return fmt.Errorf("creating %s: %w", repoPath, err)
		}
	}

	return nil
}

func updateChartValues(ctx context.Context, client *github.Client, owner, repo, branch string) error {
	chartName := "multi-env-app"

	// Update dev environment values
	devValuesPath := fmt.Sprintf("charts/%s/env/dev-values.yaml", chartName)
	devValuesContent, err := readFixture("multi-env-app/env/dev-values-updated.yaml")
	if err != nil {
		return fmt.Errorf("reading dev values fixture: %w", err)
	}

	// Get existing file to get its SHA
	existingFile, _, _, err := client.Repositories.GetContents(
		ctx,
		owner,
		repo,
		devValuesPath,
		&github.RepositoryContentGetOptions{
			Ref: branch,
		},
	)
	if err != nil {
		return fmt.Errorf("getting existing dev values: %w", err)
	}

	opts := &github.RepositoryContentFileOptions{
		Message: github.Ptr("Update dev environment - increase replicas to 2"),
		Content: devValuesContent,
		Branch:  github.Ptr(branch),
		SHA:     existingFile.SHA,
	}

	_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, devValuesPath, opts)
	if err != nil {
		return fmt.Errorf("updating dev values: %w", err)
	}

	// Update prod environment values
	prodValuesPath := fmt.Sprintf("charts/%s/env/prod-values.yaml", chartName)
	prodValuesContent, err := readFixture("multi-env-app/env/prod-values-updated.yaml")
	if err != nil {
		return fmt.Errorf("reading prod values fixture: %w", err)
	}

	existingFile, _, _, err = client.Repositories.GetContents(
		ctx,
		owner,
		repo,
		prodValuesPath,
		&github.RepositoryContentGetOptions{
			Ref: branch,
		},
	)
	if err != nil {
		return fmt.Errorf("getting existing prod values: %w", err)
	}

	opts = &github.RepositoryContentFileOptions{
		Message: github.Ptr("Update prod environment - bump version to 2.0.0 and increase replicas"),
		Content: prodValuesContent,
		Branch:  github.Ptr(branch),
		SHA:     existingFile.SHA,
	}

	_, _, err = client.Repositories.UpdateFile(ctx, owner, repo, prodValuesPath, opts)
	return err
}
