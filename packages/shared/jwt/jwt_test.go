package jwt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func newTestServer(iss *Issuer) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(iss.JWKS())
	})
	return httptest.NewServer(mux)
}

func TestVerifyValidToken(t *testing.T) {
	iss, _ := NewIssuer("vehicle-demo.identity-service")
	srv := newTestServer(iss)
	defer srv.Close()
	v := NewVerifier(srv.URL+"/.well-known/jwks.json", "vehicle-demo.identity-service")

	token, err := iss.Issue("service:vehicle-service", "audit-service", "audit.write")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := v.Verify(context.Background(), token, "audit-service", "audit.write")
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if claims.Subject != "service:vehicle-service" {
		t.Errorf("sub = %q", claims.Subject)
	}
	if claims.ID == "" {
		t.Error("jti (ID) should be set")
	}
}

func TestVerifyRejects(t *testing.T) {
	iss, _ := NewIssuer("vehicle-demo.identity-service")
	srv := newTestServer(iss)
	defer srv.Close()
	jwks := srv.URL + "/.well-known/jwks.json"

	token, _ := iss.Issue("service:vehicle-service", "audit-service", "audit.write")

	t.Run("wrong audience", func(t *testing.T) {
		v := NewVerifier(jwks, "vehicle-demo.identity-service")
		if _, err := v.Verify(context.Background(), token, "vehicle-service", "audit.write"); err == nil {
			t.Error("expected audience mismatch to fail")
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		v := NewVerifier(jwks, "some-other-issuer")
		if _, err := v.Verify(context.Background(), token, "audit-service", "audit.write"); err == nil {
			t.Error("expected issuer mismatch to fail")
		}
	})

	t.Run("missing scope", func(t *testing.T) {
		v := NewVerifier(jwks, "vehicle-demo.identity-service")
		if _, err := v.Verify(context.Background(), token, "audit-service", "vehicle.register"); err == nil {
			t.Error("expected missing scope to fail")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		v := NewVerifier(jwks, "vehicle-demo.identity-service")
		expired := signWith(iss, gojwt.RegisteredClaims{
			Issuer:    "vehicle-demo.identity-service",
			Subject:   "service:vehicle-service",
			Audience:  gojwt.ClaimStrings{"audit-service"},
			IssuedAt:  gojwt.NewNumericDate(time.Now().Add(-10 * time.Minute)),
			ExpiresAt: gojwt.NewNumericDate(time.Now().Add(-5 * time.Minute)),
			ID:        uuid.NewString(),
		}, "audit.write")
		if _, err := v.Verify(context.Background(), expired, "audit-service", "audit.write"); err == nil {
			t.Error("expected expired token to fail")
		}
	})

	t.Run("tampered signature", func(t *testing.T) {
		v := NewVerifier(jwks, "vehicle-demo.identity-service")
		tampered := token[:len(token)-3] + "AAA"
		if _, err := v.Verify(context.Background(), tampered, "audit-service", "audit.write"); err == nil {
			t.Error("expected tampered token to fail")
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		other, _ := NewIssuer("vehicle-demo.identity-service")
		foreign, _ := other.Issue("service:x", "audit-service", "audit.write")
		v := NewVerifier(jwks, "vehicle-demo.identity-service")
		if _, err := v.Verify(context.Background(), foreign, "audit-service", "audit.write"); err == nil {
			t.Error("expected token signed by unknown key to fail")
		}
	})
}

// signWith signs custom claims using the issuer's private key (internal test access).
func signWith(iss *Issuer, rc gojwt.RegisteredClaims, scope string) string {
	claims := Claims{Scope: scope, RegisteredClaims: rc}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = iss.keyID
	s, _ := tok.SignedString(iss.private)
	return s
}

func TestStaticVerifier(t *testing.T) {
	iss, _ := NewIssuer("vehicle-demo.identity-service")
	// A static verifier validates the issuer's own tokens with no HTTP/JWKS call.
	v := NewStaticVerifier("vehicle-demo.identity-service", iss.PublicKeys())

	token, _ := iss.Issue("service:vehicle-factory", "identity-service", "bootstrap.provision")
	claims, err := v.Verify(context.Background(), token, "identity-service", "bootstrap.provision")
	if err != nil {
		t.Fatalf("static verifier rejected valid token: %v", err)
	}
	if claims.Subject != "service:vehicle-factory" {
		t.Errorf("sub = %q", claims.Subject)
	}

	// A token signed by a different (unknown) key must be rejected without refresh.
	other, _ := NewIssuer("vehicle-demo.identity-service")
	foreign, _ := other.Issue("service:x", "identity-service", "bootstrap.provision")
	if _, err := v.Verify(context.Background(), foreign, "identity-service", "bootstrap.provision"); err == nil {
		t.Error("static verifier accepted a token signed by an unknown key")
	}
}
