package githubin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

const testSecret = "test-webhook-secret"

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// blockingUseCase blocks Execute until the caller signals via gate.
type blockingUseCase struct {
	gate      chan struct{}
	active    atomic.Int32
	completed atomic.Int32
}

func (b *blockingUseCase) Execute(_ context.Context, _ domain.PRContext) error {
	b.active.Add(1)
	<-b.gate
	b.active.Add(-1)
	b.completed.Add(1)
	return nil
}

// noopUseCase returns immediately.
type noopUseCase struct{}

func (noopUseCase) Execute(context.Context, domain.PRContext) error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func buildPRPayload(tb testing.TB, action string) []byte {
	tb.Helper()
	payload := map[string]any{
		"action": action,
		"number": 1,
		"pull_request": map[string]any{
			"head": map[string]any{"ref": "feature", "sha": "abc123"},
			"base": map[string]any{"ref": "main"},
		},
		"repository": map[string]any{
			"name":  "my-repo",
			"owner": map[string]any{"login": "my-org"},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		tb.Fatalf("marshal payload: %v", err)
	}
	return body
}

func newSignedPRRequest(
	tb testing.TB,
	secret, action string,
) *http.Request {
	tb.Helper()
	body := buildPRPayload(tb, action)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sign(body, secret))
	req.Header.Set("X-GitHub-Event", "pull_request")
	return req
}

func newTestHandler(uc interface {
	Execute(context.Context, domain.PRContext) error
},
) *WebhookHandler {
	return NewWebhookHandler(
		uc,
		testSecret,
		slog.New(slog.NewTextHandler(
			&discardWriter{},
			&slog.HandlerOptions{Level: slog.LevelError},
		)),
	)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func waitFor(
	t *testing.T,
	cond func() bool,
	timeout time.Duration,
	msg string,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", msg)
		}
		runtime.Gosched()
	}
}

// ---------------------------------------------------------------------------
// Handler routing tests
// ---------------------------------------------------------------------------

func TestHandler_InvalidSignature(t *testing.T) {
	h := newTestHandler(noopUseCase{})

	body := buildPRPayload(t, "opened")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	req.Header.Set("X-GitHub-Event", "pull_request")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestHandler_NonPREvent(t *testing.T) {
	h := newTestHandler(noopUseCase{})

	// Send a push event — handler should return 200 (ignored).
	body := []byte(`{"ref":"refs/heads/main"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sign(body, testSecret))
	req.Header.Set("X-GitHub-Event", "push")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestHandler_IgnoredActions(t *testing.T) {
	h := newTestHandler(noopUseCase{})

	for _, action := range []string{"closed", "edited", "labeled", "assigned"} {
		t.Run(action, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, newSignedPRRequest(t, testSecret, action))

			if rr.Code != http.StatusOK {
				t.Fatalf("action %q: got %d, want 200", action, rr.Code)
			}
		})
	}
}

func TestHandler_AcceptedActions(t *testing.T) {
	for _, action := range []string{"opened", "synchronize", "reopened"} {
		t.Run(action, func(t *testing.T) {
			h := newTestHandler(noopUseCase{})
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, newSignedPRRequest(t, testSecret, action))

			if rr.Code != http.StatusAccepted {
				t.Fatalf("action %q: got %d, want 202", action, rr.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Semaphore non-blocking test
// ---------------------------------------------------------------------------

func TestSemaphore_Returns202Immediately(t *testing.T) {
	uc := &blockingUseCase{gate: make(chan struct{})}
	h := newTestHandler(uc)
	h.sem = make(chan struct{}, 1) // single slot to prove non-blocking

	// Saturate the single slot.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newSignedPRRequest(t, testSecret, "opened"))
	waitFor(t, func() bool { return uc.active.Load() == 1 }, 2*time.Second,
		"slot should fill")

	// Fire another request — must return 202 without blocking on the semaphore.
	start := time.Now()
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, newSignedPRRequest(t, testSecret, "opened"))
	elapsed := time.Since(start)

	if rr2.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rr2.Code)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("ServeHTTP took %v; semaphore appears to block the handler", elapsed)
	}

	// Clean up goroutines.
	for range 2 {
		uc.gate <- struct{}{}
	}
	waitFor(t, func() bool { return uc.completed.Load() == 2 }, 2*time.Second,
		"cleanup")
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkWebhookHandler(b *testing.B) {
	h := newTestHandler(noopUseCase{})
	body := buildPRPayload(b, "opened")
	sig := sign(body, testSecret)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(
			http.MethodPost,
			"/webhook",
			bytes.NewReader(body),
		)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request")
		h.ServeHTTP(rr, req)
	}
}
