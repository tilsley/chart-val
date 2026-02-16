// Package githubin handles incoming GitHub webhook events.
package githubin

import (
	"context"
	"log/slog"
	"net/http"

	gogithub "github.com/google/go-github/v68/github"
	"go.opentelemetry.io/otel/trace"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

// WebhookHandler handles incoming GitHub webhook events.
type WebhookHandler struct {
	useCase       ports.DiffUseCase
	webhookSecret []byte
	logger        *slog.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(uc ports.DiffUseCase, secret string, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		useCase:       uc,
		webhookSecret: []byte(secret),
		logger:        logger,
	}
}

// ServeHTTP validates the webhook signature, parses the event, and
// dispatches the diff use case in a goroutine (responds 202 immediately).
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload, err := gogithub.ValidatePayload(r, h.webhookSecret)
	if err != nil {
		h.logger.Error("invalid webhook signature", "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	event, err := gogithub.ParseWebHook(gogithub.WebHookType(r), payload)
	if err != nil {
		h.logger.Error("failed to parse webhook", "error", err)
		http.Error(w, "failed to parse webhook", http.StatusBadRequest)
		return
	}

	prEvent, ok := event.(*gogithub.PullRequestEvent)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}

	action := prEvent.GetAction()
	if action != "opened" && action != "synchronize" && action != "reopened" {
		w.WriteHeader(http.StatusOK)
		return
	}

	pr := domain.PRContext{
		Owner:    prEvent.GetRepo().GetOwner().GetLogin(),
		Repo:     prEvent.GetRepo().GetName(),
		PRNumber: prEvent.GetNumber(),
		BaseRef:  prEvent.GetPullRequest().GetBase().GetRef(),
		HeadRef:  prEvent.GetPullRequest().GetHead().GetRef(),
		HeadSHA:  prEvent.GetPullRequest().GetHead().GetSHA(),
	}

	h.logger.Info("processing pull request",
		"owner", pr.Owner,
		"repo", pr.Repo,
		"pr", pr.PRNumber,
		"action", action,
	)

	// Dispatch asynchronously — GitHub has a 10s webhook timeout.
	// Embed the inbound request's span context as the remote parent so all
	// async spans share the same trace ID (single trace in Grafana/Jaeger).
	// Only the Go context is detached (avoiding cancellation); the trace continues.
	ctx := trace.ContextWithRemoteSpanContext(context.Background(),
		trace.SpanContextFromContext(r.Context()),
	)
	go func() {
		if err := h.useCase.Execute(ctx, pr); err != nil {
			h.logger.Error("diff execution failed",
				"owner", pr.Owner,
				"repo", pr.Repo,
				"pr", pr.PRNumber,
				"error", err,
			)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}
