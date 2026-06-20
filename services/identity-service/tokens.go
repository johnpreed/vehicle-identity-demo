package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/middleware"
	"vehicle-identity-demo/packages/shared/models"
)

// issuerPrincipal is the subject identity-service uses for itself — it is the sole
// token issuer (Ed25519 signer + JWKS) in the system.
const issuerPrincipal = "service:identity-service"

// auditTokenEvent records a service_token_issued decision (ALLOW or DENY) so the
// audit log shows every workload that authenticated to call another service. It is
// fire-and-forget so token issuance never blocks on audit-service. The reason is
// composed to make the issuer -> recipient -> audience relationship explicit, and
// the issuer is recorded in metadata. `detail` is only used to explain a denial.
func (a *App) auditTokenEvent(r *http.Request, actorType, actorID, audience, detail string, allow bool, meta map[string]any) {
	decision := models.DecisionDeny
	reason := fmt.Sprintf("%s denied a token to %s for audience %s: %s", issuerPrincipal, actorID, audience, detail)
	if allow {
		decision = models.DecisionAllow
		reason = fmt.Sprintf("%s issued a token to %s for audience %s", issuerPrincipal, actorID, audience)
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["issuer"] = issuerPrincipal
	corr := httpx.CorrelationID(r.Context())
	go a.audit.Emit(context.Background(), corr, models.AuditEvent{
		ActorType:    actorType,
		ActorID:      actorID,
		Action:       "service_token_issued",
		ResourceType: "service",
		ResourceID:   audience,
		Decision:     decision,
		Reason:       reason,
		Metadata:     meta,
	})
}

// handleProvisionBootstrap registers (or rotates) a vehicle's factory bootstrap
// credential. It models the manufacturing "burn-in" step: a trusted factory
// workload presents a JWT scoped `bootstrap.provision` (audience identity-service)
// and registers the VIN + device secret the vehicle will later use to call home.
func (a *App) handleProvisionBootstrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VIN    string `json:"vin"`
		Secret string `json:"bootstrap_secret"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.VIN == "" || req.Secret == "" {
		httpx.WriteError(w, http.StatusBadRequest, "vin and bootstrap_secret required")
		return
	}
	if err := a.store.UpsertBootstrapCredential(r.Context(), req.VIN, req.Secret); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := "service:vehicle-factory"
	if claims := middleware.ClaimsFrom(r.Context()); claims != nil {
		actor = claims.Subject
	}
	corr := httpx.CorrelationID(r.Context())
	go a.audit.Emit(context.Background(), corr, models.AuditEvent{
		ActorType:    models.ActorService,
		ActorID:      actor,
		Action:       "bootstrap_provisioned",
		ResourceType: "vehicle_bootstrap_credential",
		ResourceID:   req.VIN,
		Decision:     models.DecisionAllow,
		Reason:       "factory bootstrap credential provisioned",
	})
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"vin": req.VIN, "status": "provisioned"})
}

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
	if audience == "" {
		audience = "vehicle-service"
	}
	want, err := a.store.GetBootstrapSecret(r.Context(), vin)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if want == "" || want != secret {
		a.auditTokenEvent(r, models.ActorVehicle, "vehicle:"+vin, audience,
			"invalid bootstrap credential", false, map[string]any{"grant_type": "vehicle_bootstrap"})
		httpx.WriteError(w, http.StatusUnauthorized, "invalid bootstrap credential")
		return
	}
	sub := "service:simulated-vehicle:" + vin
	scope := "vehicle.register vehicle.heartbeat"
	token, jti, exp, err := a.issuer.IssueWithID(sub, audience, scope)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.auditTokenEvent(r, models.ActorVehicle, sub, audience, "", true,
		map[string]any{"grant_type": "vehicle_bootstrap", "scope": scope, "jti": jti, "expires_at": exp})
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
		a.auditTokenEvent(r, models.ActorService, "client:"+clientID, audience,
			"invalid client credentials", false, map[string]any{"grant_type": "service_credential"})
		httpx.WriteError(w, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	if !contains(allowedAuds, audience) {
		a.auditTokenEvent(r, models.ActorService, subject, audience,
			"audience not allowed for client", false, map[string]any{"grant_type": "service_credential", "scope": scope})
		httpx.WriteError(w, http.StatusForbidden, "audience not allowed for client")
		return
	}
	for _, s := range strings.Fields(scope) {
		if !contains(allowedScopes, s) {
			a.auditTokenEvent(r, models.ActorService, subject, audience,
				"scope not allowed: "+s, false, map[string]any{"grant_type": "service_credential", "scope": scope})
			httpx.WriteError(w, http.StatusForbidden, "scope not allowed: "+s)
			return
		}
	}
	token, jti, exp, err := a.issuer.IssueWithID(subject, audience, scope)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.auditTokenEvent(r, models.ActorService, subject, audience, "", true,
		map[string]any{"grant_type": "service_credential", "scope": scope, "jti": jti, "expires_at": exp})
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
