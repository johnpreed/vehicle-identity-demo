// Package audit is a client for the audit-service: it writes audit events
// (POST /audit). It is token-source agnostic — callers supply a TokenProvider that
// returns an audit.write bearer token, so both services that obtain the token over
// HTTP (via the identity client) and the identity-service that self-issues it can
// share one implementation.
package audit

import (
	"context"
	"log"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/models"
)

// TokenProvider returns a short-lived bearer token with the audit.write scope.
type TokenProvider func(ctx context.Context) (string, error)

// Client writes audit events to a single audit-service instance.
type Client struct {
	baseURL string
	token   TokenProvider
}

// New returns an audit client for baseURL using token to authenticate writes.
func New(baseURL string, token TokenProvider) *Client {
	return &Client{baseURL: baseURL, token: token}
}

// Write posts a single audit event. It returns an error so callers can decide how
// to handle failures (see Emit for fire-and-forget semantics).
func (c *Client) Write(ctx context.Context, correlationID string, ev models.AuditEvent) error {
	ev.CorrelationID = correlationID
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	_, err = httpx.PostJSON(ctx, c.baseURL+"/audit", token, correlationID, ev, nil)
	return err
}

// Emit writes an event best-effort: failures are logged and never block the caller.
func (c *Client) Emit(ctx context.Context, correlationID string, ev models.AuditEvent) {
	if err := c.Write(ctx, correlationID, ev); err != nil {
		log.Printf("audit write error (correlation=%s action=%s): %v", correlationID, ev.Action, err)
	}
}

// EmitWithRetry retries for ~60s. Used for startup events emitted before
// audit-service has necessarily finished starting.
func (c *Client) EmitWithRetry(ctx context.Context, correlationID string, ev models.AuditEvent) {
	for attempt := 0; attempt < 30; attempt++ {
		if err := c.Write(ctx, correlationID, ev); err == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("audit write (startup) gave up: action=%s", ev.Action)
}
