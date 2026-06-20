package main

import (
	"context"
	"log"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
	sjwt "vehicle-identity-demo/packages/shared/jwt"
	"vehicle-identity-demo/packages/shared/models"
)

// auditEmitter lets identity-service record its own security-relevant operations
// (token issuance, bootstrap provisioning, signing-key lifecycle) to audit-service.
// It self-issues a short-lived audit.write token directly from the local issuer —
// this bypasses the public /service-token handler, so audit writes never recurse
// into another service_token_issued event.
type auditEmitter struct {
	issuer   *sjwt.Issuer
	auditURL string
}

func newAuditEmitter(issuer *sjwt.Issuer, auditURL string) *auditEmitter {
	return &auditEmitter{issuer: issuer, auditURL: auditURL}
}

func (e *auditEmitter) writeToken() (string, error) {
	return e.issuer.Issue("service:identity-service", models.AudAuditService, models.ScopeAuditWrite)
}

// emit writes an audit event best-effort; failures are logged, never block callers.
func (e *auditEmitter) emit(ctx context.Context, corr string, ev models.AuditEvent) {
	ev.CorrelationID = corr
	token, err := e.writeToken()
	if err != nil {
		log.Printf("audit token error (action=%s): %v", ev.Action, err)
		return
	}
	if _, err := httpx.PostJSON(ctx, e.auditURL+"/audit", token, corr, ev, nil); err != nil {
		log.Printf("audit write error (action=%s): %v", ev.Action, err)
	}
}

// emitWithRetry retries for ~60s. Used for startup events emitted before
// audit-service has necessarily finished starting.
func (e *auditEmitter) emitWithRetry(ctx context.Context, corr string, ev models.AuditEvent) {
	ev.CorrelationID = corr
	for attempt := 0; attempt < 30; attempt++ {
		token, err := e.writeToken()
		if err == nil {
			if _, err = httpx.PostJSON(ctx, e.auditURL+"/audit", token, corr, ev, nil); err == nil {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("audit write (startup) gave up: action=%s", ev.Action)
}
