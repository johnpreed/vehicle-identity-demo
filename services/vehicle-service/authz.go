package main

import "vehicle-identity-demo/packages/shared/models"

// Subject is the resolved caller for an authorization decision. A subject is
// either a consumer (with a Role on the target vehicle) or staff (with a Persona).
type Subject struct {
	Kind        string // models.ActorConsumer / ActorStaff / ActorVehicle / ActorService
	UserID      string
	Username    string
	Role        string // consumer role on the target vehicle (owner/co-owner/driver/viewer), or ""
	Persona     string // staff persona, or ""
	StepUpFresh bool   // a passkey step-up occurred within the freshness window
}

// Decision is the explicit result of an authorization check.
type Decision struct {
	Allowed bool
	Reason  string
}

func allow() Decision             { return Decision{Allowed: true, Reason: "authorized"} }
func deny(reason string) Decision { return Decision{Allowed: false, Reason: reason} }

func (s Subject) isConsumer() bool { return s.Kind == models.ActorConsumer }
func (s Subject) isStaff() bool    { return s.Kind == models.ActorStaff }

func (s Subject) hasRole(roles ...string) bool {
	if !s.isConsumer() {
		return false
	}
	for _, r := range roles {
		if s.Role == r {
			return true
		}
	}
	return false
}

func (s Subject) hasPersona(p string) bool { return s.isStaff() && s.Persona == p }

// CanViewStatus: owner, co-owner, driver, viewer.
func CanViewStatus(s Subject) Decision {
	if s.hasRole(models.RoleOwner, models.RoleCoOwner, models.RoleDriver, models.RoleViewer) {
		return allow()
	}
	return deny("view_status requires a vehicle role")
}

// CanUnlock: owner, co-owner, driver.
func CanUnlock(s Subject) Decision {
	if s.hasRole(models.RoleOwner, models.RoleCoOwner, models.RoleDriver) {
		return allow()
	}
	return deny("unlock_doors requires owner, co-owner, or driver")
}

// CanStartClimate: owner, co-owner, driver.
func CanStartClimate(s Subject) Decision {
	if s.hasRole(models.RoleOwner, models.RoleCoOwner, models.RoleDriver) {
		return allow()
	}
	return deny("start_climate requires owner, co-owner, or driver")
}

// CanStartVehicle: owner, co-owner, or driver AND a fresh passkey step-up. High-risk command.
func CanStartVehicle(s Subject) Decision {
	if !s.hasRole(models.RoleOwner, models.RoleCoOwner, models.RoleDriver) {
		return deny("start_vehicle requires owner, co-owner, or driver")
	}
	if !s.StepUpFresh {
		return deny("start_vehicle requires a recent passkey step-up")
	}
	return allow()
}

// CanInviteDriver: owner or co-owner.
func CanInviteDriver(s Subject) Decision {
	if s.hasRole(models.RoleOwner, models.RoleCoOwner) {
		return allow()
	}
	return deny("invite_driver requires owner or co-owner")
}

// CanAssignOwner: staff sales_support.
func CanAssignOwner(s Subject) Decision {
	if s.hasPersona(models.PersonaSalesSupport) {
		return allow()
	}
	return deny("assign_owner requires the sales_support persona")
}

// CanCreateVehicle: staff manufacturing.
func CanCreateVehicle(s Subject) Decision {
	if s.hasPersona(models.PersonaManufacturing) {
		return allow()
	}
	return deny("create_vehicle requires the manufacturing persona")
}
