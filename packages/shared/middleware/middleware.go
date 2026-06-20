// Package middleware contains shared HTTP middleware: correlation-id handling,
// CORS for the local web apps, and service-to-service JWT scope enforcement.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/jwt"
)

// Correlation ensures every request has a correlation id (from the inbound header
// or freshly generated), stores it in the context, and echoes it on the response.
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.CorrelationFromRequest(r)
		w.Header().Set(httpx.CorrelationHeader, id)
		ctx := httpx.WithCorrelationID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CORS allows the local web origins to call the service with credentials.
func CORS(allowedOrigins ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+httpx.CorrelationHeader+", X-Staff-Persona")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type claimsKey struct{}

// RequireScope returns middleware that verifies a bearer JWT for the given audience
// and scope, storing the claims in the request context on success.
func RequireScope(v *jwt.Verifier, audience, scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearer(r)
			if token == "" {
				httpx.WriteError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := v.Verify(r.Context(), token, audience, scope)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "invalid token: "+err.Error())
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFrom returns the verified JWT claims stored by RequireScope, if any.
func ClaimsFrom(ctx context.Context) *jwt.Claims {
	c, _ := ctx.Value(claimsKey{}).(*jwt.Claims)
	return c
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[len("bearer "):])
	}
	return ""
}
