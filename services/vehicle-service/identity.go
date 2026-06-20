package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
)

// IdentityClient introspects consumer sessions by calling identity-service /me.
// This keeps identity-service the single source of truth for sessions and step-up.
type IdentityClient struct {
	baseURL string
	client  *http.Client
}

func NewIdentityClient(baseURL string) *IdentityClient {
	return &IdentityClient{baseURL: baseURL, client: &http.Client{Timeout: 5 * time.Second}}
}

// MeResult is the subset of identity-service /me used for authorization.
type MeResult struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	StepUpFresh bool `json:"step_up_fresh"`
}

// Introspect resolves the user behind a session cookie value, plus step-up freshness.
func (c *IdentityClient) Introspect(ctx context.Context, sessionToken, correlationID string) (*MeResult, error) {
	if sessionToken == "" {
		return nil, fmt.Errorf("no session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "vid_session="+sessionToken)
	req.Header.Set(httpx.CorrelationHeader, correlationID)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session introspection failed: %d", resp.StatusCode)
	}
	var me MeResult
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return nil, err
	}
	return &me, nil
}
