// Package identity is a client for the identity-service: it wraps workload token
// issuance (POST /service-token), factory bootstrap provisioning
// (POST /bootstrap/provision), and consumer session introspection (GET /me).
package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
)

// Client talks to a single identity-service instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for the identity-service at baseURL (e.g. http://identity-service:8081).
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Token is an issued workload JWT and its expiry.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

type tokenResponse struct {
	Token string `json:"token"`
}

// tokenLifetime mirrors the issuer's fixed 5-minute lifetime; callers only need a
// safe lower bound to drive cache refresh.
const tokenLifetime = 5 * time.Minute

// ServiceToken exchanges a backend service's client credentials for a short-lived
// JWT (the service_credential grant).
func (c *Client) ServiceToken(ctx context.Context, clientID, clientSecret, audience, scope string) (Token, error) {
	return c.token(ctx, map[string]string{
		"grant_type":    "service_credential",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"audience":      audience,
		"scope":         scope,
	})
}

// BootstrapToken exchanges a vehicle's VIN + factory bootstrap secret for a
// short-lived JWT (the vehicle_bootstrap grant).
func (c *Client) BootstrapToken(ctx context.Context, vin, secret, audience string) (Token, error) {
	return c.token(ctx, map[string]string{
		"grant_type":       "vehicle_bootstrap",
		"vin":              vin,
		"bootstrap_secret": secret,
		"audience":         audience,
	})
}

func (c *Client) token(ctx context.Context, body map[string]string) (Token, error) {
	var out tokenResponse
	if _, err := httpx.PostJSON(ctx, c.baseURL+"/service-token", "", httpx.CorrelationID(ctx), body, &out); err != nil {
		return Token{}, err
	}
	return Token{Value: out.Token, ExpiresAt: time.Now().Add(tokenLifetime)}, nil
}

// ProvisionBootstrap registers (factory "burn-in") a vehicle's bootstrap credential.
// The bearer token must carry the bootstrap.provision scope for audience identity-service.
func (c *Client) ProvisionBootstrap(ctx context.Context, bearer, vin, secret string) error {
	_, err := httpx.PostJSON(ctx, c.baseURL+"/bootstrap/provision", bearer, httpx.CorrelationID(ctx),
		map[string]string{"vin": vin, "bootstrap_secret": secret}, nil)
	return err
}

// Session is the subset of identity-service GET /me used for authorization.
type Session struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	StepUpFresh bool `json:"step_up_fresh"`
}

// Introspect resolves the user behind a consumer session cookie value, along with
// passkey step-up freshness. identity-service stays the source of truth for sessions.
func (c *Client) Introspect(ctx context.Context, sessionToken, correlationID string) (*Session, error) {
	if sessionToken == "" {
		return nil, fmt.Errorf("no session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "vid_session="+sessionToken)
	req.Header.Set(httpx.CorrelationHeader, correlationID)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session introspection failed: %d", resp.StatusCode)
	}
	var s Session
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}
