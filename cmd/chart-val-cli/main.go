package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/google/go-github/v68/github"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		token      = flag.String("token", "", "GitHub personal access token (or use GITHUB_TOKEN env var)")
		webhookURL = flag.String("url", "http://localhost:8080/webhook", "Webhook URL")
		secret     = flag.String("secret", "", "Webhook secret for signing (read from WEBHOOK_SECRET env var if not set)")
		installID  = flag.Int64("installation-id", 0, "GitHub App installation ID (read from GITHUB_INSTALLATION_ID env var if not set)")
	)
	flag.Parse()

	// Get token from flag or environment
	if *token == "" {
		*token = os.Getenv("GITHUB_TOKEN")
	}
	if *token == "" {
		fmt.Fprintf(os.Stderr, "Error: GitHub token required\n")
		fmt.Fprintf(os.Stderr, "Provide via -token flag or GITHUB_TOKEN env var\n")
		return fmt.Errorf("missing github token")
	}

	// Get webhook secret from flag or environment
	if *secret == "" {
		*secret = os.Getenv("WEBHOOK_SECRET")
	}
	if *secret == "" {
		fmt.Fprintf(os.Stderr, "Error: Webhook secret required\n")
		fmt.Fprintf(os.Stderr, "Provide via -secret flag or WEBHOOK_SECRET env var\n")
		return fmt.Errorf("missing webhook secret")
	}

	// Get installation ID from flag or environment
	if *installID == 0 {
		if idStr := os.Getenv("GITHUB_INSTALLATION_ID"); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
			}
			*installID = id
		}
	}
	if *installID == 0 {
		fmt.Fprintf(os.Stderr, "Error: GitHub App installation ID required\n")
		fmt.Fprintf(os.Stderr, "Provide via -installation-id flag or GITHUB_INSTALLATION_ID env var\n")
		return fmt.Errorf("missing installation ID")
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: chart-val-cli <pr-url> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  GITHUB_TOKEN=ghp_xxx chart-val-cli https://github.com/owner/repo/pull/123\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
		return fmt.Errorf("missing PR URL")
	}

	prURL := args[0]

	// Parse PR URL: https://github.com/owner/repo/pull/123
	owner, repo, prNum, err := parsePRURL(prURL)
	if err != nil {
		return fmt.Errorf("parsing PR URL: %w", err)
	}

	// Create GitHub client with token
	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(*token)

	// Fetch PR details from GitHub
	fmt.Printf("Fetching PR details from GitHub...\n")
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNum)
	if err != nil {
		return fmt.Errorf("fetching PR: %w", err)
	}

	baseRef := pr.GetBase().GetRef()
	headRef := pr.GetHead().GetRef()
	headSHA := pr.GetHead().GetSHA()

	// Construct webhook payload
	payload := map[string]interface{}{
		"action": "synchronize",
		"number": prNum,
		"pull_request": map[string]interface{}{
			"number": prNum,
			"base": map[string]interface{}{
				"ref": baseRef,
			},
			"head": map[string]interface{}{
				"ref": headRef,
				"sha": headSHA,
			},
		},
		"repository": map[string]interface{}{
			"name":  repo,
			"owner": map[string]interface{}{"login": owner},
		},
		"installation": map[string]interface{}{
			"id": *installID,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	// Sign the payload
	signature := signPayload(payloadBytes, *secret)

	// Send webhook request
	fmt.Printf("\nSending webhook to %s...\n", *webhookURL)
	fmt.Printf("  Owner: %s\n", owner)
	fmt.Printf("  Repo: %s\n", repo)
	fmt.Printf("  PR: #%d\n", prNum)
	fmt.Printf("  Base: %s\n", baseRef)
	fmt.Printf("  Head: %s (%s)\n", headRef, headSHA)
	fmt.Printf("  Installation ID: %d\n", *installID)
	fmt.Println()

	req, err := http.NewRequest("POST", *webhookURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256="+signature)
	req.Header.Set("X-GitHub-Delivery", "test-delivery-"+fmt.Sprintf("%d", prNum))

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		fmt.Printf("✓ Webhook accepted (status %d)\n", resp.StatusCode)
		if len(body) > 0 {
			fmt.Printf("Response: %s\n", string(body))
		}
		fmt.Printf("\nCheck your GitHub PR for the results!\n")
		fmt.Printf("%s\n", prURL)
		return nil
	}

	fmt.Printf("✗ Webhook failed (status %d)\n", resp.StatusCode)
	if len(body) > 0 {
		fmt.Printf("Response: %s\n", string(body))
	}
	return fmt.Errorf("webhook returned status %d", resp.StatusCode)
}

// parsePRURL extracts owner, repo, and PR number from a GitHub PR URL
// Handles formats:
//   - https://github.com/owner/repo/pull/123
//   - https://github.com/owner/repo/pull/123/changes
//   - https://github.com/owner/repo/pull/123/files
func parsePRURL(url string) (string, string, int, error) {
	// Handle both http and https URLs, with optional trailing paths
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)(?:/.*)?$`)
	matches := re.FindStringSubmatch(url)

	if len(matches) != 4 {
		return "", "", 0, fmt.Errorf("invalid PR URL format, expected: https://github.com/owner/repo/pull/123, got: %s", url)
	}

	owner := matches[1]
	repo := matches[2]
	prNum, err := strconv.Atoi(matches[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number: %w", err)
	}

	return owner, repo, prNum, nil
}

// signPayload creates HMAC SHA256 signature for the payload
func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
