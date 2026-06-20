package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceTokenAndBootstrapToken(t *testing.T) {
	var lastBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service-token" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		_ = json.NewEncoder(w).Encode(tokenResponse{Token: "tok-" + lastBody["grant_type"]})
	}))
	defer srv.Close()
	c := New(srv.URL)

	st, err := c.ServiceToken(context.Background(), "vehicle-service", "secret", "audit-service", "audit.write")
	if err != nil {
		t.Fatal(err)
	}
	if st.Value != "tok-service_credential" {
		t.Errorf("service token = %q", st.Value)
	}
	if lastBody["client_id"] != "vehicle-service" || lastBody["scope"] != "audit.write" {
		t.Errorf("unexpected service-credential body: %v", lastBody)
	}
	if st.ExpiresAt.Before(time.Now()) {
		t.Error("expiry should be in the future")
	}

	bt, err := c.BootstrapToken(context.Background(), "VIN-1", "boot", "vehicle-service")
	if err != nil {
		t.Fatal(err)
	}
	if bt.Value != "tok-vehicle_bootstrap" {
		t.Errorf("bootstrap token = %q", bt.Value)
	}
	if lastBody["vin"] != "VIN-1" || lastBody["bootstrap_secret"] != "boot" {
		t.Errorf("unexpected bootstrap body: %v", lastBody)
	}
}

func TestCachedTokenRefresh(t *testing.T) {
	var calls int32
	// First call returns an already-expired token (forces refresh next time);
	// second returns a long-lived token that should then be cached.
	cached := NewCachedToken(func(ctx context.Context) (Token, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return Token{Value: "first", ExpiresAt: time.Now().Add(-time.Second)}, nil
		}
		return Token{Value: "second", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})

	if v, _ := cached.Value(context.Background()); v != "first" {
		t.Fatalf("first value = %q", v)
	}
	// Expired -> refresh.
	if v, _ := cached.Value(context.Background()); v != "second" {
		t.Fatalf("second value = %q", v)
	}
	// Cached now -> no further refresh.
	if v, _ := cached.Value(context.Background()); v != "second" {
		t.Fatalf("third value = %q", v)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("refresh called %d times, want 2", got)
	}
}

func TestIntrospect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, _ := r.Cookie("vid_session"); c == nil || c.Value != "sess-abc" {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"alice"},"step_up_fresh":true}`))
	}))
	defer srv.Close()
	c := New(srv.URL)

	s, err := c.Introspect(context.Background(), "sess-abc", "corr-1")
	if err != nil {
		t.Fatal(err)
	}
	if s.User.Username != "alice" || !s.StepUpFresh {
		t.Errorf("unexpected session: %+v", s)
	}
	if _, err := c.Introspect(context.Background(), "wrong", "corr-1"); err == nil {
		t.Error("expected error for bad session")
	}
	if _, err := c.Introspect(context.Background(), "", "corr-1"); err == nil {
		t.Error("expected error for empty session")
	}
}
