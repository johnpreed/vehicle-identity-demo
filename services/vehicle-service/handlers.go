package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	auditclient "vehicle-identity-demo/packages/clients/audit"
	identityclient "vehicle-identity-demo/packages/clients/identity"
	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/models"
)

const resourceVehicle = "vehicle"

// App holds vehicle-service dependencies.
type App struct {
	store    *Store
	identity *identityclient.Client
	audit    *auditclient.Client
}

func validPersona(p string) bool {
	switch p {
	case models.PersonaManufacturing, models.PersonaSalesSupport, models.PersonaSecurityAuditor:
		return true
	}
	return false
}

// staffSubject builds a staff Subject from the X-Staff-Persona header (demo auth).
func staffSubject(r *http.Request) (Subject, bool) {
	p := r.Header.Get("X-Staff-Persona")
	if !validPersona(p) {
		return Subject{}, false
	}
	return Subject{Kind: models.ActorStaff, Persona: p}, true
}

// consumer introspects the session cookie via identity-service.
func (a *App) consumer(r *http.Request) (*identityclient.Session, bool) {
	c, err := r.Cookie("vid_session")
	if err != nil {
		return nil, false
	}
	me, err := a.identity.Introspect(r.Context(), c.Value, httpx.CorrelationID(r.Context()))
	if err != nil || me == nil || me.User.ID == "" {
		return nil, false
	}
	return me, true
}

// consumerSubject builds a consumer Subject including their role on the vehicle.
func (a *App) consumerSubject(r *http.Request, vehicleID string) (Subject, *identityclient.Session, bool) {
	me, ok := a.consumer(r)
	if !ok {
		return Subject{}, nil, false
	}
	role, _ := a.store.GetRole(r.Context(), vehicleID, me.User.Username)
	return Subject{
		Kind:        models.ActorConsumer,
		UserID:      me.User.ID,
		Username:    me.User.Username,
		Role:        role,
		StepUpFresh: me.StepUpFresh,
	}, me, true
}

func (a *App) emit(r *http.Request, actorType, actorID, action, resourceID string, dec Decision, meta map[string]any) {
	a.audit.Emit(r.Context(), httpx.CorrelationID(r.Context()), models.AuditEvent{
		ActorType:    actorType,
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceVehicle,
		ResourceID:   resourceID,
		Decision:     decisionString(dec.Allowed),
		Reason:       dec.Reason,
		Metadata:     meta,
	})
}

func decisionString(allowed bool) string {
	if allowed {
		return models.DecisionAllow
	}
	return models.DecisionDeny
}

func newCode(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// ---- staff: create ----

func (a *App) handleCreate(w http.ResponseWriter, r *http.Request) {
	sub, ok := staffSubject(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "valid X-Staff-Persona required")
		return
	}
	var req struct {
		VIN   string `json:"vin"`
		Model string `json:"model"`
	}
	_ = httpx.ReadJSON(r, &req)
	if req.Model == "" {
		req.Model = "Demo EV"
	}
	dec := CanCreateVehicle(sub)
	if !dec.Allowed {
		a.emit(r, models.ActorStaff, sub.Persona, "create_vehicle", req.VIN, dec, nil)
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	if req.VIN == "" {
		req.VIN = "VIN-" + newCode(4)
	}
	if existing, _ := a.store.GetByVIN(r.Context(), req.VIN); existing != nil {
		httpx.WriteError(w, http.StatusConflict, "VIN already exists")
		return
	}
	claimCode := newCode(3)
	v, err := a.store.Create(r.Context(), req.VIN, req.Model, claimCode)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.emit(r, models.ActorStaff, sub.Persona, "create_vehicle", v.ID, dec,
		map[string]any{"vin": v.VIN, "claim_code": v.ClaimCode})
	httpx.WriteJSON(w, http.StatusCreated, v)
}

// ---- staff: assign owner ----

func (a *App) handleAssignOwner(w http.ResponseWriter, r *http.Request) {
	sub, ok := staffSubject(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "valid X-Staff-Persona required")
		return
	}
	id := r.PathValue("id")
	var req struct {
		Username string `json:"username"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.Username == "" {
		httpx.WriteError(w, http.StatusBadRequest, "username required")
		return
	}
	dec := CanAssignOwner(sub)
	if !dec.Allowed {
		a.emit(r, models.ActorStaff, sub.Persona, "assign_owner", id, dec, map[string]any{"username": req.Username})
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	v, _ := a.store.GetByID(r.Context(), id)
	if v == nil {
		httpx.WriteError(w, http.StatusNotFound, "vehicle not found")
		return
	}
	if err := a.store.AssignOwner(r.Context(), v.ID, req.Username); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.emit(r, models.ActorStaff, sub.Persona, "assign_owner", v.ID, dec, map[string]any{"username": req.Username})
	v, _ = a.store.GetByID(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, v)
}

// ---- list / detail ----

func (a *App) handleListVehicles(w http.ResponseWriter, r *http.Request) {
	if sub, ok := staffSubject(r); ok {
		_ = sub
		vehicles, err := a.store.ListAll(r.Context())
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"vehicles": vehicles})
		return
	}
	me, ok := a.consumer(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "sign in or provide a staff persona")
		return
	}
	vehicles, err := a.store.ListForUser(r.Context(), me.User.Username)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range vehicles {
		vehicles[i].ClaimCode = "" // consumers don't need to see claim codes
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"vehicles": vehicles})
}

func (a *App) handleGetVehicle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, _ := a.store.GetByID(r.Context(), id)
	if v == nil {
		httpx.WriteError(w, http.StatusNotFound, "vehicle not found")
		return
	}

	var sub Subject
	staff := false
	if s, ok := staffSubject(r); ok {
		sub, staff = s, true
	} else {
		s, _, ok := a.consumerSubject(r, id)
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		sub = s
	}

	if dec := CanViewStatus(sub); !dec.Allowed {
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	grants, _ := a.store.ListGrants(r.Context(), id)
	if !staff {
		v.ClaimCode = ""
	}
	resp := map[string]any{"vehicle": v, "grants": grants, "your_role": sub.Role}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ---- claim ----

// handleLookup lets a signed-in consumer resolve a CLAIMABLE vehicle by VIN so they
// can claim it without knowing its internal id. Only CLAIMABLE vehicles are revealed.
func (a *App) handleLookup(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.consumer(r); !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	vin := r.URL.Query().Get("vin")
	v, _ := a.store.GetByVIN(r.Context(), vin)
	if v == nil || v.LifecycleStatus != "CLAIMABLE" {
		httpx.WriteError(w, http.StatusNotFound, "no claimable vehicle with that VIN")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": v.ID, "vin": v.VIN, "model": v.Model, "lifecycle_status": v.LifecycleStatus,
	})
}

func (a *App) handleClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me, ok := a.consumer(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req struct {
		ClaimCode string `json:"claim_code"`
	}
	_ = httpx.ReadJSON(r, &req)

	v, _ := a.store.GetByID(r.Context(), id)
	if v == nil {
		httpx.WriteError(w, http.StatusNotFound, "vehicle not found")
		return
	}
	actor := me.User.Username
	if v.LifecycleStatus != "CLAIMABLE" || v.OwnershipState != "UNASSIGNED" {
		dec := deny("vehicle is not claimable")
		a.emit(r, models.ActorConsumer, actor, "claim_vehicle", v.ID, dec, nil)
		httpx.WriteError(w, http.StatusConflict, dec.Reason)
		return
	}
	if req.ClaimCode == "" || !strings.EqualFold(req.ClaimCode, v.ClaimCode) {
		dec := deny("invalid claim code")
		a.emit(r, models.ActorConsumer, actor, "claim_vehicle", v.ID, dec, nil)
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	if err := a.store.Claim(r.Context(), v, me.User.ID, me.User.Username); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.emit(r, models.ActorConsumer, actor, "claim_vehicle", v.ID, allow(), map[string]any{"role": "owner"})
	v, _ = a.store.GetByID(r.Context(), id)
	v.ClaimCode = ""
	httpx.WriteJSON(w, http.StatusOK, v)
}

// ---- invite / accept ----

func (a *App) handleInvite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sub, me, ok := a.consumerSubject(r, id)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil || req.Username == "" {
		httpx.WriteError(w, http.StatusBadRequest, "username required")
		return
	}
	if req.Role == "" {
		req.Role = models.RoleDriver
	}
	switch req.Role {
	case models.RoleCoOwner, models.RoleDriver, models.RoleViewer:
	default:
		httpx.WriteError(w, http.StatusBadRequest, "role must be co-owner, driver, or viewer")
		return
	}
	dec := CanInviteDriver(sub)
	if !dec.Allowed {
		a.emit(r, models.ActorConsumer, me.User.Username, "invite_driver", id, dec, map[string]any{"username": req.Username, "role": req.Role})
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	code := newCode(4)
	inv, err := a.store.CreateInvitation(r.Context(), id, req.Username, req.Role, me.User.Username, code)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.emit(r, models.ActorConsumer, me.User.Username, "invite_driver", id, dec, map[string]any{"username": req.Username, "role": req.Role, "code": code})
	httpx.WriteJSON(w, http.StatusCreated, inv)
}

func (a *App) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	me, ok := a.consumer(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	invs, err := a.store.ListInvitationsForUsername(r.Context(), me.User.Username, "pending")
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invitations": invs})
}

func (a *App) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	me, ok := a.consumer(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	inv, _ := a.store.GetInvitation(r.Context(), code)
	if inv == nil || inv.Status != "pending" {
		httpx.WriteError(w, http.StatusNotFound, "no pending invitation with that code")
		return
	}
	if !strings.EqualFold(inv.InvitedUsername, me.User.Username) {
		dec := deny("invitation was issued to a different user")
		a.emit(r, models.ActorConsumer, me.User.Username, "accept_invite", inv.VehicleID, dec, map[string]any{"code": code})
		httpx.WriteError(w, http.StatusForbidden, dec.Reason)
		return
	}
	if err := a.store.AcceptInvitation(r.Context(), inv, me.User.ID, me.User.Username); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.emit(r, models.ActorConsumer, me.User.Username, "accept_invite", inv.VehicleID, allow(), map[string]any{"role": inv.Role})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"vehicle_id": inv.VehicleID, "role": inv.Role})
}
