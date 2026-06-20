package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vehicle-identity-demo/packages/shared/models"
)

func TestWriteAttachesTokenAndCorrelation(t *testing.T) {
	type captured struct {
		auth  string
		corr  string
		event models.AuditEvent
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev models.AuditEvent
		_ = json.NewDecoder(r.Body).Decode(&ev)
		got <- captured{auth: r.Header.Get("Authorization"), corr: r.Header.Get("X-Correlation-Id"), event: ev}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, func(ctx context.Context) (string, error) { return "test-token", nil })
	err := c.Write(context.Background(), "corr-123", models.AuditEvent{
		ActorType: models.ActorConsumer,
		ActorID:   "driver-bob",
		Action:    "start_vehicle",
		Decision:  models.DecisionDeny,
		Reason:    "start_vehicle requires a recent passkey step-up",
	})
	if err != nil {
		t.Fatal(err)
	}

	c2 := <-got
	if c2.auth != "Bearer test-token" {
		t.Errorf("authorization = %q", c2.auth)
	}
	if c2.corr != "corr-123" || c2.event.CorrelationID != "corr-123" {
		t.Errorf("correlation header=%q body=%q", c2.corr, c2.event.CorrelationID)
	}
	if c2.event.Decision != models.DecisionDeny || c2.event.Action != "start_vehicle" {
		t.Errorf("unexpected event: %+v", c2.event)
	}
}

func TestWritePropagatesTokenError(t *testing.T) {
	c := New("http://unused", func(ctx context.Context) (string, error) {
		return "", context.DeadlineExceeded
	})
	if err := c.Write(context.Background(), "corr", models.AuditEvent{Action: "x"}); err == nil {
		t.Error("expected token error to propagate")
	}
}
