package main

import (
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

// waUser adapts our User + stored credentials to the webauthn.User interface.
type waUser struct {
	user  *User
	creds []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u *waUser) WebAuthnName() string                       { return u.user.Username }
func (u *waUser) WebAuthnDisplayName() string                { return u.user.DisplayName }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// ceremony holds in-flight WebAuthn session data between start and finish.
type ceremony struct {
	userID   string
	username string // set for sign-up ceremonies (user row created on finish)
	token    string // session token for step-up ceremonies
	session  webauthn.SessionData
	expires  time.Time
}

// ceremonyStore is an in-memory, TTL'd store of in-flight ceremonies. A single
// identity-service instance owns all ceremonies, which keeps the demo simple.
type ceremonyStore struct {
	mu sync.Mutex
	m  map[string]ceremony
}

func newCeremonyStore() *ceremonyStore {
	cs := &ceremonyStore{m: map[string]ceremony{}}
	go cs.gc()
	return cs
}

func (cs *ceremonyStore) put(c ceremony) string {
	id := uuid.NewString()
	c.expires = time.Now().Add(5 * time.Minute)
	cs.mu.Lock()
	cs.m[id] = c
	cs.mu.Unlock()
	return id
}

func (cs *ceremonyStore) take(id string) (ceremony, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, ok := cs.m[id]
	delete(cs.m, id)
	if ok && time.Now().After(c.expires) {
		return ceremony{}, false
	}
	return c, ok
}

func (cs *ceremonyStore) gc() {
	for range time.Tick(time.Minute) {
		cs.mu.Lock()
		for id, c := range cs.m {
			if time.Now().After(c.expires) {
				delete(cs.m, id)
			}
		}
		cs.mu.Unlock()
	}
}

func credentialID(c *webauthn.Credential) string {
	return base64.RawURLEncoding.EncodeToString(c.ID)
}

func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

const sessionCookieName = "vid_session"
