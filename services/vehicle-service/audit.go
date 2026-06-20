package main

import (
	"context"
	"log"
	"sync"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/models"
)

// AuditClient writes audit events to audit-service. It first obtains a short-lived
// S2S JWT (audit.write scope) from identity-service using vehicle-service's own
// workload credentials, demonstrating service-to-service workload identity.
type AuditClient struct {
	identityURL  string
	auditURL     string
	clientID     string
	clientSecret string

	mu    sync.Mutex
	token string
	exp   time.Time
}

func NewAuditClient(identityURL, auditURL, clientID, clientSecret string) *AuditClient {
	return &AuditClient{identityURL: identityURL, auditURL: auditURL, clientID: clientID, clientSecret: clientSecret}
}

func (c *AuditClient) tokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp.Add(-1*time.Minute)) {
		return c.token, nil
	}
	var out struct {
		Token string `json:"token"`
	}
	body := map[string]string{
		"grant_type":    "service_credential",
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"audience":      models.AudAuditService,
		"scope":         models.ScopeAuditWrite,
	}
	if _, err := httpx.PostJSON(ctx, c.identityURL+"/service-token", "", httpx.CorrelationID(ctx), body, &out); err != nil {
		return "", err
	}
	c.token = out.Token
	c.exp = time.Now().Add(5 * time.Minute)
	return c.token, nil
}

// Emit writes an audit event. Failures are logged but never block the caller.
func (c *AuditClient) Emit(ctx context.Context, correlationID string, e models.AuditEvent) {
	e.CorrelationID = correlationID
	token, err := c.tokenFor(ctx)
	if err != nil {
		log.Printf("audit token error (correlation=%s action=%s): %v", correlationID, e.Action, err)
		return
	}
	if _, err := httpx.PostJSON(ctx, c.auditURL+"/audit", token, correlationID, e, nil); err != nil {
		log.Printf("audit write error (correlation=%s action=%s): %v", correlationID, e.Action, err)
	}
}
