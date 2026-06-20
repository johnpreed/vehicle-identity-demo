// Package vehicle is a client for the vehicle-service. It wraps the device-facing
// calls a vehicle makes (register, heartbeat) and fleet discovery (list).
package vehicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
)

// Vehicle is the subset of vehicle-service's vehicle returned to clients.
type Vehicle struct {
	ID                string `json:"id"`
	VIN               string `json:"vin"`
	LifecycleStatus   string `json:"lifecycle_status"`
	ConnectivityState string `json:"connectivity_state"`
}

// Client talks to a single vehicle-service instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for the vehicle-service at baseURL.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Register registers a device with vehicle-service. The bearer token must carry the
// vehicle.register scope and a subject bound to the VIN.
func (c *Client) Register(ctx context.Context, bearer, correlationID, vin string) (Vehicle, error) {
	var v Vehicle
	_, err := httpx.PostJSON(ctx, c.baseURL+"/vehicles/register", bearer, correlationID,
		map[string]string{"vin": vin}, &v)
	return v, err
}

// Heartbeat sends a liveness heartbeat for a registered device.
func (c *Client) Heartbeat(ctx context.Context, bearer, correlationID, vehicleID string) error {
	_, err := httpx.PostJSON(ctx, c.baseURL+"/vehicles/"+vehicleID+"/heartbeat", bearer, correlationID, nil, nil)
	return err
}

// List returns the fleet visible to the given staff persona (e.g. "manufacturing").
func (c *Client) List(ctx context.Context, persona string) ([]Vehicle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/vehicles", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Staff-Persona", persona)
	req.Header.Set(httpx.CorrelationHeader, httpx.CorrelationID(ctx))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list vehicles returned %d", resp.StatusCode)
	}
	var body struct {
		Vehicles []Vehicle `json:"vehicles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Vehicles, nil
}
