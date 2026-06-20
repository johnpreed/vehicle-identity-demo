package main

import (
	"net/http"
	"strings"

	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/middleware"
	"vehicle-identity-demo/packages/shared/models"
)

// handleUnlock: unlock_doors (owner, co-owner, driver).
func (a *App) handleUnlock(w http.ResponseWriter, r *http.Request) {
	a.runCommand(w, r, "unlock_doors", CanUnlock, func(vehicleID string) error {
		return a.store.SetAccessState(r.Context(), vehicleID, "UNLOCKED")
	})
}

// handleStartClimate: start_climate (owner, co-owner, driver).
func (a *App) handleStartClimate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	_ = httpx.ReadJSON(r, &req)
	mode := strings.ToUpper(req.Mode)
	switch mode {
	case "HEATING", "COOLING", "AUTO", "OFF":
	case "":
		mode = "AUTO"
	default:
		httpx.WriteError(w, http.StatusBadRequest, "mode must be HEATING, COOLING, AUTO, or OFF")
		return
	}
	a.runCommand(w, r, "start_climate", CanStartClimate, func(vehicleID string) error {
		return a.store.SetClimateState(r.Context(), vehicleID, mode)
	})
}

// runCommand is the shared path for non-high-risk consumer commands: resolve the
// subject, evaluate the explicit authorization check, audit the decision, and apply
// the side effect only when allowed.
func (a *App) runCommand(w http.ResponseWriter, r *http.Request, action string, check func(Subject) Decision, apply func(vehicleID string) error) {
	id := r.PathValue("id")
	sub, me, ok := a.consumerSubject(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	v, _ := a.store.GetByID(r.Context(), id)
	if v == nil {
		httpx.WriteError(w, http.StatusNotFound, "vehicle not found")
		return
	}
	dec := check(sub)
	_ = a.store.InsertCommand(r.Context(), id, action, "", me.User.Username, decisionString(dec.Allowed), dec.Reason)
	a.emit(r, models.ActorConsumer, me.User.Username, action, id, dec, nil)
	if !dec.Allowed {
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	if err := apply(id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, _ = a.store.GetByID(r.Context(), id)
	v.ClaimCode = ""
	httpx.WriteJSON(w, http.StatusOK, v)
}

// handleStartVehicle is the high-risk command: requires owner/co-owner AND a fresh
// passkey step-up AND an idempotency key. It always audits the decision.
func (a *App) handleStartVehicle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sub, me, ok := a.consumerSubject(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = httpx.ReadJSON(r, &req)
	if req.IdempotencyKey == "" {
		httpx.WriteError(w, http.StatusBadRequest, "idempotency_key required for start_vehicle")
		return
	}
	v, _ := a.store.GetByID(r.Context(), id)
	if v == nil {
		httpx.WriteError(w, http.StatusNotFound, "vehicle not found")
		return
	}

	// Idempotency: a replayed key returns the prior decision without re-executing.
	if prior, found, _ := a.store.FindCommand(r.Context(), id, "start_vehicle", req.IdempotencyKey); found {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"replayed": true, "decision": prior, "vehicle": v,
		})
		return
	}

	dec := CanStartVehicle(sub)
	meta := map[string]any{"idempotency_key": req.IdempotencyKey, "step_up_fresh": sub.StepUpFresh}
	_ = a.store.InsertCommand(r.Context(), id, "start_vehicle", req.IdempotencyKey, me.User.Username, decisionString(dec.Allowed), dec.Reason)
	a.emit(r, models.ActorConsumer, me.User.Username, "start_vehicle", id, dec, meta)
	if !dec.Allowed {
		status := http.StatusForbidden
		if strings.Contains(dec.Reason, "step-up") {
			status = http.StatusPreconditionRequired // 428: client should perform step-up
		}
		httpx.WriteError(w, status, dec.Reason)
		return
	}
	if err := a.store.SetPowerState(r.Context(), id, "STARTED"); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, _ = a.store.GetByID(r.Context(), id)
	v.ClaimCode = ""
	httpx.WriteJSON(w, http.StatusOK, v)
}

// ---- device endpoints (S2S JWT authenticated) ----

func vinFromSubject(sub string) string {
	return strings.TrimPrefix(sub, "service:simulated-vehicle:")
}

// handleRegister is called by the simulated vehicle with a vehicle.register scoped
// token. The VIN in the token subject must match the registering VIN.
func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r.Context())
	var req struct {
		VIN string `json:"vin"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.VIN == "" {
		httpx.WriteError(w, http.StatusBadRequest, "vin required")
		return
	}
	if claims == nil || vinFromSubject(claims.Subject) != req.VIN {
		httpx.WriteError(w, http.StatusForbidden, "token subject does not match VIN")
		return
	}
	v, _ := a.store.GetByVIN(r.Context(), req.VIN)
	if v == nil {
		// Not yet spawned by manufacturing staff.
		httpx.WriteError(w, http.StatusNotFound, "vehicle has not been manufactured/spawned")
		return
	}
	if v.LifecycleStatus == "MANUFACTURED" || v.LifecycleStatus == "PROVISIONED" || v.LifecycleStatus == "REGISTERED" {
		if err := a.store.RegisterDevice(r.Context(), v.ID, claims.Subject); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.emit(r, models.ActorVehicle, claims.Subject, "register_vehicle", v.ID, allow(),
			map[string]any{"vin": v.VIN})
	} else {
		// Already registered/claimed: treat as a heartbeat (idempotent re-call).
		_ = a.store.Heartbeat(r.Context(), v.ID)
	}
	v, _ = a.store.GetByID(r.Context(), v.ID)
	v.ClaimCode = ""
	httpx.WriteJSON(w, http.StatusOK, v)
}

// handleHeartbeat is called periodically by the simulated vehicle.
func (a *App) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.Heartbeat(r.Context(), id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
