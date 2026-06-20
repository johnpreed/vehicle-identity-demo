package main

import (
	"net/http"
	"strings"

	"vehicle-identity-demo/packages/shared/httpx"
)

// handleJWKS publishes the issuer's Ed25519 public key for service-to-service verification.
func (a *App) handleJWKS(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, a.issuer.JWKS())
}

// handleServiceToken mints short-lived S2S tokens for two workload grant types:
//   - vehicle_bootstrap: a vehicle proves VIN + factory bootstrap secret.
//   - service_credential: a backend service proves client_id + client_secret.
func (a *App) handleServiceToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GrantType    string `json:"grant_type"`
		Audience     string `json:"audience"`
		Scope        string `json:"scope"`
		VIN          string `json:"vin"`
		Secret       string `json:"bootstrap_secret"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}

	switch req.GrantType {
	case "vehicle_bootstrap":
		a.issueVehicleToken(w, r, req.VIN, req.Secret, req.Audience)
	case "service_credential":
		a.issueServiceToken(w, r, req.ClientID, req.ClientSecret, req.Audience, req.Scope)
	default:
		httpx.WriteError(w, http.StatusBadRequest, "unsupported grant_type")
	}
}

func (a *App) issueVehicleToken(w http.ResponseWriter, r *http.Request, vin, secret, audience string) {
	if vin == "" || secret == "" {
		httpx.WriteError(w, http.StatusBadRequest, "vin and bootstrap_secret required")
		return
	}
	want, err := a.store.GetBootstrapSecret(r.Context(), vin)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if want == "" || want != secret {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid bootstrap credential")
		return
	}
	if audience == "" {
		audience = "vehicle-service"
	}
	sub := "service:simulated-vehicle:" + vin
	scope := "vehicle.register vehicle.heartbeat"
	token, err := a.issuer.Issue(sub, audience, scope)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"token": token, "sub": sub, "scope": scope, "audience": audience})
}

func (a *App) issueServiceToken(w http.ResponseWriter, r *http.Request, clientID, clientSecret, audience, scope string) {
	if clientID == "" || clientSecret == "" {
		httpx.WriteError(w, http.StatusBadRequest, "client_id and client_secret required")
		return
	}
	subject, secret, allowedScopes, allowedAuds, err := a.store.GetServiceIdentity(r.Context(), clientID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if subject == "" || secret != clientSecret {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	if !contains(allowedAuds, audience) {
		httpx.WriteError(w, http.StatusForbidden, "audience not allowed for client")
		return
	}
	for _, s := range strings.Fields(scope) {
		if !contains(allowedScopes, s) {
			httpx.WriteError(w, http.StatusForbidden, "scope not allowed: "+s)
			return
		}
	}
	token, err := a.issuer.Issue(subject, audience, scope)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"token": token, "sub": subject, "scope": scope, "audience": audience})
}

func contains(spaceList, want string) bool {
	for _, s := range strings.Fields(spaceList) {
		if s == want {
			return true
		}
	}
	return false
}
