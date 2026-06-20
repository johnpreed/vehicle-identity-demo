package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vehicle-identity-demo/packages/shared/models"
)

// TestAuditEmitWritesDeniedEvent verifies that a denied command produces an audit
// write carrying the DENY decision, correlation id, and a bearer token obtained
// from identity-service with the audit.write scope.
func TestAuditEmitWritesDeniedEvent(t *testing.T) {
	// Fake identity-service: issues a token for the service_credential grant.
	var tokenReq map[string]string
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&tokenReq)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
	}))
	defer identity.Close()

	// Fake audit-service: captures the authorization header and event body.
	type captured struct {
		auth  string
		corr  string
		event models.AuditEvent
	}
	got := make(chan captured, 1)
	audit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e models.AuditEvent
		_ = json.NewDecoder(r.Body).Decode(&e)
		got <- captured{auth: r.Header.Get("Authorization"), corr: r.Header.Get("X-Correlation-Id"), event: e}
		w.WriteHeader(http.StatusCreated)
	}))
	defer audit.Close()

	client := NewAuditClient(identity.URL, audit.URL, "vehicle-service", "secret")
	client.Emit(context.Background(), "corr-123", models.AuditEvent{
		ActorType:    models.ActorConsumer,
		ActorID:      "driver-bob",
		Action:       "start_vehicle",
		ResourceType: "vehicle",
		ResourceID:   "veh-1",
		Decision:     models.DecisionDeny,
		Reason:       "start_vehicle requires owner or co-owner",
	})

	c := <-got
	if c.event.Decision != models.DecisionDeny {
		t.Errorf("decision = %q, want DENY", c.event.Decision)
	}
	if c.event.Action != "start_vehicle" {
		t.Errorf("action = %q, want start_vehicle", c.event.Action)
	}
	if c.corr != "corr-123" {
		t.Errorf("correlation id = %q, want corr-123", c.corr)
	}
	if c.auth != "Bearer test-token" {
		t.Errorf("authorization = %q, want Bearer test-token", c.auth)
	}
	if tokenReq["scope"] != models.ScopeAuditWrite || tokenReq["audience"] != models.AudAuditService {
		t.Errorf("token request scope/audience = %q/%q, want %q/%q",
			tokenReq["scope"], tokenReq["audience"], models.ScopeAuditWrite, models.AudAuditService)
	}
}
