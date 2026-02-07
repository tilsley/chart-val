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

	"github.com/nathantilsley/chart-sentry/internal/platform/config"
	ghclient "github.com/nathantilsley/chart-sentry/internal/platform/github"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		webhookURL = flag.String("url", "http://localhost:8080/webhook", "Webhook URL")
		secret     = flag.String("secret", "test", "Webhook secret for signing")
		installID  = flag.Int64("installation-id", 108584464, "GitHub App installation ID")
	)
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: chart-sentry-cli <pr-url> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  chart-sentry-cli https://github.com/owner/repo/pull/123\n")
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

	// Load config for GitHub API access
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Create GitHub client
	clientFactory, err := ghclient.NewClientFactory(cfg.GitHubAppID, cfg.GitHubPrivateKey)
	if err != nil {
		return fmt.Errorf("creating github client factory: %w", err)
	}

	client := clientFactory.ForInstallation(*installID)

	// Fetch PR details from GitHub
	fmt.Printf("Fetching PR details from GitHub...\n")
	pr, _, err := client.PullRequests.Get(context.Background(), owner, repo, prNum)
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
// Expected format: https://github.com/owner/repo/pull/123
func parsePRURL(url string) (string, string, int, error) {
	// Handle both http and https URLs
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
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
