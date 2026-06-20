// Package httpx provides small HTTP helpers shared across services: correlation-id
// propagation, JSON request/response helpers, and a service-to-service JSON client.
package httpx

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// CorrelationHeader is the header used to propagate a correlation id across services.
const CorrelationHeader = "X-Correlation-Id"

type ctxKey string

const correlationKey ctxKey = "correlation_id"

// NewCorrelationID returns a fresh correlation id.
func NewCorrelationID() string { return uuid.NewString() }

// WithCorrelationID stores a correlation id in the context.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID returns the correlation id from the context, or "" if absent.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey).(string)
	return id
}

// CorrelationFromRequest returns the request's correlation id, generating one if missing.
func CorrelationFromRequest(r *http.Request) string {
	if id := r.Header.Get(CorrelationHeader); id != "" {
		return id
	}
	return NewCorrelationID()
}
