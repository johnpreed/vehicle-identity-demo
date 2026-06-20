// Package models holds cross-service constants and shared payload types.
package models

// Service-to-service JWT scopes.
const (
	ScopeAuditWrite         = "audit.write"
	ScopeAuditRead          = "audit.read"
	ScopeVehicleRegister    = "vehicle.register"
	ScopeVehicleHeartbeat   = "vehicle.heartbeat"
	ScopeBootstrapProvision = "bootstrap.provision"
)

// JWT audiences (the service that consumes the token).
const (
	AudAuditService    = "audit-service"
	AudVehicleService  = "vehicle-service"
	AudIdentityService = "identity-service"
)

// Staff personas.
const (
	PersonaManufacturing   = "manufacturing"
	PersonaSalesSupport    = "sales_support"
	PersonaSecurityAuditor = "security_auditor"
)

// Consumer vehicle roles.
const (
	RoleOwner   = "owner"
	RoleCoOwner = "co-owner"
	RoleDriver  = "driver"
	RoleViewer  = "viewer"
)

// Authorization decisions (recorded on every audited action).
const (
	DecisionAllow = "ALLOW"
	DecisionDeny  = "DENY"
)

// Actor types for audit events.
const (
	ActorConsumer = "consumer"
	ActorStaff    = "staff"
	ActorService  = "service"
	ActorVehicle  = "vehicle"
)

// AuditEvent is the request body accepted by audit-service POST /audit.
type AuditEvent struct {
	CorrelationID string         `json:"correlation_id"`
	ActorType     string         `json:"actor_type"`
	ActorID       string         `json:"actor_id"`
	Action        string         `json:"action"`
	ResourceType  string         `json:"resource_type"`
	ResourceID    string         `json:"resource_id"`
	Decision      string         `json:"decision"`
	Reason        string         `json:"reason"`
	Metadata      map[string]any `json:"metadata"`
}
