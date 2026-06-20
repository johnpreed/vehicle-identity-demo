// Package jwt issues and verifies short-lived Ed25519 (EdDSA) JWTs and exposes
// the public key as a JWKS for service-to-service verification. No HMAC is used.
package jwt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Lifetime is the fixed lifetime of every issued service token.
const Lifetime = 5 * time.Minute

// Claims is the JWT payload: standard registered claims plus a space-delimited scope.
type Claims struct {
	Scope string `json:"scope"`
	gojwt.RegisteredClaims
}

// HasScope reports whether the claims grant the required scope.
func (c *Claims) HasScope(required string) bool {
	for _, s := range strings.Fields(c.Scope) {
		if s == required {
			return true
		}
	}
	return false
}

// JWK is a single Ed25519 public key in JWKS form.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	X   string `json:"x"`
}

// JWKS is a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// Issuer signs tokens with an Ed25519 private key.
type Issuer struct {
	issuer  string
	keyID   string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

// NewIssuer generates a fresh Ed25519 keypair and returns an Issuer. Generating
// the key on startup keeps the demo zero-config; the public key is published via JWKS.
func NewIssuer(issuer string) (*Issuer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Issuer{issuer: issuer, keyID: keyID(pub), private: priv, public: pub}, nil
}

// Issue creates a signed token for the given subject, audience and scope.
func (i *Issuer) Issue(sub, aud, scope string) (string, error) {
	now := time.Now()
	claims := Claims{
		Scope: scope,
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   sub,
			Audience:  gojwt.ClaimStrings{aud},
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(now.Add(Lifetime)),
			ID:        uuid.NewString(),
		},
	}
	tok := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = i.keyID
	return tok.SignedString(i.private)
}

// JWKS returns the issuer's public key as a JWKS document.
func (i *Issuer) JWKS() JWKS {
	return JWKS{Keys: []JWK{{
		Kty: "OKP",
		Crv: "Ed25519",
		Kid: i.keyID,
		Alg: "EdDSA",
		Use: "sig",
		X:   base64.RawURLEncoding.EncodeToString(i.public),
	}}}
}

func keyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

func (k JWK) publicKey() (ed25519.PublicKey, error) {
	if k.Kty != "OKP" || k.Crv != "Ed25519" {
		return nil, fmt.Errorf("unsupported key type %s/%s", k.Kty, k.Crv)
	}
	raw, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key length")
	}
	return ed25519.PublicKey(raw), nil
}
