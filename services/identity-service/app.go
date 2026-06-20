package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"vehicle-identity-demo/packages/clients/audit"
	"vehicle-identity-demo/packages/shared/httpx"
	sjwt "vehicle-identity-demo/packages/shared/jwt"
)

const sessionTTL = 12 * time.Hour

// App holds identity-service dependencies.
type App struct {
	store    *Store
	web      *webauthn.WebAuthn
	ceremony *ceremonyStore
	issuer   *sjwt.Issuer
	audit    *audit.Client
}

func (a *App) loadWAUser(r *http.Request, userID string) (*waUser, error) {
	u, err := a.store.GetUserByID(r.Context(), userID)
	if err != nil || u == nil {
		return nil, err
	}
	creds, err := a.store.Credentials(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	return &waUser{user: u, creds: creds}, nil
}

// ---- sign up ----

func (a *App) handleSignupStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.Username == "" {
		httpx.WriteError(w, http.StatusBadRequest, "username required")
		return
	}
	if existing, _ := a.store.GetUserByUsername(r.Context(), req.Username); existing != nil {
		httpx.WriteError(w, http.StatusConflict, "username already registered")
		return
	}
	// The user row is only created on finish, so an abandoned passkey prompt
	// leaves no orphan and the same username can be retried.
	u := &User{ID: uuid.NewString(), Username: req.Username, DisplayName: req.Username}
	wu := &waUser{user: u}
	options, session, err := a.web.BeginRegistration(wu)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := a.ceremony.put(ceremony{userID: u.ID, username: u.Username, session: *session})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ceremony_id": id, "options": options})
}

func (a *App) handleSignupFinish(w http.ResponseWriter, r *http.Request) {
	cer, cred, ok := a.consumeCeremony(w, r)
	if !ok {
		return
	}
	if cer.username == "" {
		httpx.WriteError(w, http.StatusBadRequest, "not a sign-up ceremony")
		return
	}
	wu := &waUser{user: &User{ID: cer.userID, Username: cer.username, DisplayName: cer.username}}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(cred))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid attestation: "+err.Error())
		return
	}
	credential, err := a.web.CreateCredential(wu, cer.session, parsed)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "registration failed: "+err.Error())
		return
	}
	if err := a.store.CreateUserWithID(r.Context(), wu.user.ID, wu.user.Username); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.AddCredential(r.Context(), wu.user.ID, credentialID(credential), credential); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.startSession(w, r, wu.user)
}

// ---- sign in ----

func (a *App) handleSigninStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.Username == "" {
		httpx.WriteError(w, http.StatusBadRequest, "username required")
		return
	}
	u, _ := a.store.GetUserByUsername(r.Context(), req.Username)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "no such user")
		return
	}
	wu, err := a.loadWAUser(r, u.ID)
	if err != nil || len(wu.creds) == 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "no passkey registered")
		return
	}
	options, session, err := a.web.BeginLogin(wu)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := a.ceremony.put(ceremony{userID: u.ID, session: *session})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ceremony_id": id, "options": options})
}

func (a *App) handleSigninFinish(w http.ResponseWriter, r *http.Request) {
	cer, cred, ok := a.consumeCeremony(w, r)
	if !ok {
		return
	}
	wu, err := a.loadWAUser(r, cer.userID)
	if err != nil || wu == nil {
		httpx.WriteError(w, http.StatusBadRequest, "unknown user")
		return
	}
	if _, err := a.validateAssertion(r.Context(), wu, cer.session, cred); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "sign-in failed: "+err.Error())
		return
	}
	a.startSession(w, r, wu.user)
}

// ---- step up ----

func (a *App) handleStepUpStart(w http.ResponseWriter, r *http.Request) {
	sess, user := a.currentSession(r)
	if sess == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	wu, err := a.loadWAUser(r, user.ID)
	if err != nil || len(wu.creds) == 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "no passkey registered")
		return
	}
	options, session, err := a.web.BeginLogin(wu)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := a.ceremony.put(ceremony{userID: user.ID, token: sess.Token, session: *session})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ceremony_id": id, "options": options})
}

func (a *App) handleStepUpFinish(w http.ResponseWriter, r *http.Request) {
	cer, cred, ok := a.consumeCeremony(w, r)
	if !ok {
		return
	}
	if cer.token == "" {
		httpx.WriteError(w, http.StatusBadRequest, "not a step-up ceremony")
		return
	}
	wu, err := a.loadWAUser(r, cer.userID)
	if err != nil || wu == nil {
		httpx.WriteError(w, http.StatusBadRequest, "unknown user")
		return
	}
	if _, err := a.validateAssertion(r.Context(), wu, cer.session, cred); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "step-up failed: "+err.Error())
		return
	}
	if err := a.store.MarkStepUp(r.Context(), cer.token); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "step_up_at": time.Now()})
}

// ---- /me ----

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, user := a.currentSession(r)
	if sess == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	fresh := sess.StepUpAt != nil && time.Since(*sess.StepUpAt) <= 5*time.Minute
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":          map[string]string{"id": user.ID, "username": user.Username},
		"step_up_fresh": fresh,
		"step_up_at":    sess.StepUpAt,
	})
}

// ---- helpers ----

func (a *App) validateAssertion(ctx context.Context, wu *waUser, session webauthn.SessionData, cred []byte) (*webauthn.Credential, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(cred))
	if err != nil {
		return nil, err
	}
	credential, err := a.web.ValidateLogin(wu, session, parsed)
	if err != nil {
		return nil, err
	}
	// Persist updated sign count / clone-warning state.
	_ = a.store.AddCredential(ctx, wu.user.ID, credentialID(credential), credential)
	return credential, nil
}

func (a *App) consumeCeremony(w http.ResponseWriter, r *http.Request) (ceremony, []byte, bool) {
	var req struct {
		CeremonyID string          `json:"ceremony_id"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.CeremonyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "ceremony_id and credential required")
		return ceremony{}, nil, false
	}
	cer, ok := a.ceremony.take(req.CeremonyID)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "unknown or expired ceremony")
		return ceremony{}, nil, false
	}
	return cer, req.Credential, true
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, user *User) {
	sess, err := a.store.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setSessionCookie(w, sess.Token, sessionTTL)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{"id": user.ID, "username": user.Username},
	})
}

func (a *App) currentSession(r *http.Request) (*Session, *User) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, nil
	}
	sess, err := a.store.GetSession(r.Context(), c.Value)
	if err != nil || sess == nil {
		return nil, nil
	}
	user, err := a.store.GetUserByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		return nil, nil
	}
	return sess, user
}
