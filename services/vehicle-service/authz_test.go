package main

import (
	"testing"

	"vehicle-identity-demo/packages/shared/models"
)

func consumer(role string, stepUp bool) Subject {
	return Subject{Kind: models.ActorConsumer, UserID: "u1", Username: "u1", Role: role, StepUpFresh: stepUp}
}

func staff(persona string) Subject {
	return Subject{Kind: models.ActorStaff, Persona: persona}
}

func TestCanViewStatus(t *testing.T) {
	allowed := []Subject{
		consumer(models.RoleOwner, false),
		consumer(models.RoleCoOwner, false),
		consumer(models.RoleDriver, false),
		consumer(models.RoleViewer, false),
	}
	for _, s := range allowed {
		if d := CanViewStatus(s); !d.Allowed {
			t.Errorf("CanViewStatus(%+v) = deny(%q), want allow", s, d.Reason)
		}
	}
	denied := []Subject{
		consumer("", false),
		staff(models.PersonaManufacturing),
		staff(models.PersonaSalesSupport),
		staff(models.PersonaSecurityAuditor),
	}
	for _, s := range denied {
		if d := CanViewStatus(s); d.Allowed {
			t.Errorf("CanViewStatus(%+v) = allow, want deny", s)
		}
	}
}

func TestCanUnlockAndClimate(t *testing.T) {
	for _, role := range []string{models.RoleOwner, models.RoleCoOwner, models.RoleDriver} {
		if d := CanUnlock(consumer(role, false)); !d.Allowed {
			t.Errorf("CanUnlock(%s) = deny, want allow", role)
		}
		if d := CanStartClimate(consumer(role, false)); !d.Allowed {
			t.Errorf("CanStartClimate(%s) = deny, want allow", role)
		}
	}
	for _, s := range []Subject{consumer(models.RoleViewer, false), staff(models.PersonaManufacturing)} {
		if d := CanUnlock(s); d.Allowed {
			t.Errorf("CanUnlock(%+v) = allow, want deny", s)
		}
	}
}

func TestCanStartVehicleRequiresAllowedRoleAndStepUp(t *testing.T) {
	cases := []struct {
		name      string
		sub       Subject
		wantAllow bool
		reasonSub string
	}{
		{"owner+fresh", consumer(models.RoleOwner, true), true, ""},
		{"co-owner+fresh", consumer(models.RoleCoOwner, true), true, ""},
		{"driver+fresh", consumer(models.RoleDriver, true), true, ""},
		{"owner+stale", consumer(models.RoleOwner, false), false, "step-up"},
		{"driver+stale", consumer(models.RoleDriver, false), false, "step-up"},
		{"viewer+fresh", consumer(models.RoleViewer, true), false, "owner, co-owner, or driver"},
		{"staff", staff(models.PersonaManufacturing), false, "owner, co-owner, or driver"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := CanStartVehicle(c.sub)
			if d.Allowed != c.wantAllow {
				t.Fatalf("allowed=%v want=%v (reason=%q)", d.Allowed, c.wantAllow, d.Reason)
			}
			if c.reasonSub != "" && !contains(d.Reason, c.reasonSub) {
				t.Fatalf("reason=%q, want to contain %q", d.Reason, c.reasonSub)
			}
		})
	}
}

func TestStaffActions(t *testing.T) {
	if d := CanCreateVehicle(staff(models.PersonaManufacturing)); !d.Allowed {
		t.Error("manufacturing should create")
	}
	if d := CanCreateVehicle(staff(models.PersonaSalesSupport)); d.Allowed {
		t.Error("sales_support should NOT create")
	}
	if d := CanAssignOwner(staff(models.PersonaSalesSupport)); !d.Allowed {
		t.Error("sales_support should assign owner")
	}
	if d := CanAssignOwner(staff(models.PersonaManufacturing)); d.Allowed {
		t.Error("manufacturing should NOT assign owner")
	}
	if d := CanInviteDriver(consumer(models.RoleOwner, false)); !d.Allowed {
		t.Error("owner should invite")
	}
	if d := CanInviteDriver(consumer(models.RoleDriver, false)); d.Allowed {
		t.Error("driver should NOT invite")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
