package jwt

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// Verifier validates tokens against a remote JWKS endpoint. It caches the keys
// and refreshes them when an unknown key id is seen or the cache is stale.
type Verifier struct {
	jwksURL string
	issuer  string
	client  *http.Client

	mu      sync.RWMutex
	keys    map[string]ed25519.PublicKey
	fetched time.Time
}

// NewVerifier returns a Verifier that fetches keys from jwksURL and requires the
// given issuer on every token.
func NewVerifier(jwksURL, issuer string) *Verifier {
	return &Verifier{
		jwksURL: jwksURL,
		issuer:  issuer,
		client:  &http.Client{Timeout: 5 * time.Second},
		keys:    map[string]ed25519.PublicKey{},
	}
}

// Verify checks signature, issuer, audience, expiry and (optionally) scope.
// It returns the parsed claims on success.
func (v *Verifier) Verify(ctx context.Context, tokenStr, expectedAudience, requiredScope string) (*Claims, error) {
	claims := &Claims{}
	_, err := gojwt.ParseWithClaims(tokenStr, claims, func(t *gojwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.publicKey(ctx, kid)
	},
		gojwt.WithValidMethods([]string{"EdDSA"}),
		gojwt.WithIssuer(v.issuer),
		gojwt.WithAudience(expectedAudience),
		gojwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if requiredScope != "" && !claims.HasScope(requiredScope) {
		return nil, fmt.Errorf("missing required scope %q", requiredScope)
	}
	return claims, nil
}

func (v *Verifier) publicKey(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	if kid == "" {
		return nil, errors.New("token missing kid")
	}
	v.mu.RLock()
	key, ok := v.keys[kid]
	stale := time.Since(v.fetched) > time.Minute
	v.mu.RUnlock()
	if ok && !stale {
		return key, nil
	}
	if err := v.refresh(ctx); err != nil && !ok {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

func (v *Verifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch returned %d", resp.StatusCode)
	}
	var set JWKS
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return err
	}
	keys := map[string]ed25519.PublicKey{}
	for _, k := range set.Keys {
		pk, err := k.publicKey()
		if err != nil {
			continue
		}
		keys[k.Kid] = pk
	}
	v.mu.Lock()
	v.keys = keys
	v.fetched = time.Now()
	v.mu.Unlock()
	return nil
}
