package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError writes a JSON error envelope: {"error": "..."}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// ReadJSON decodes the request body into v, rejecting unknown fields.
func ReadJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// PostJSON sends a JSON POST to url, attaching a bearer token and correlation id.
// If out is non-nil the response body is decoded into it. It returns the status code.
func PostJSON(ctx context.Context, url, bearer, correlationID string, body, out any) (int, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if correlationID != "" {
		req.Header.Set(CorrelationHeader, correlationID)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("POST %s -> %d: %s", url, resp.StatusCode, string(b))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
