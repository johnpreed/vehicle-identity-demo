package main

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps Postgres access for the vehicle-service.
type Store struct{ pool *pgxpool.Pool }

// Vehicle is the full multi-dimensional state of a vehicle.
type Vehicle struct {
	ID                string     `json:"id"`
	VIN               string     `json:"vin"`
	Model             string     `json:"model"`
	ClaimCode         string     `json:"claim_code,omitempty"`
	LifecycleStatus   string     `json:"lifecycle_status"`
	AccessState       string     `json:"access_state"`
	PowerState        string     `json:"power_state"`
	ClimateState      string     `json:"climate_state"`
	ConnectivityState string     `json:"connectivity_state"`
	OwnershipState    string     `json:"ownership_state"`
	CreatedAt         time.Time  `json:"created_at"`
	LastHeartbeatAt   *time.Time `json:"last_heartbeat_at"`
}

const vehicleCols = `id, vin, model, claim_code, lifecycle_status, access_state, power_state,
	climate_state, connectivity_state, ownership_state, created_at, last_heartbeat_at`

func scanVehicle(row pgx.Row) (*Vehicle, error) {
	var v Vehicle
	err := row.Scan(&v.ID, &v.VIN, &v.Model, &v.ClaimCode, &v.LifecycleStatus, &v.AccessState,
		&v.PowerState, &v.ClimateState, &v.ConnectivityState, &v.OwnershipState, &v.CreatedAt, &v.LastHeartbeatAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

func (s *Store) Spawn(ctx context.Context, vin, model, claimCode string) (*Vehicle, error) {
	id := uuid.NewString()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vehicles (id, vin, model, claim_code, lifecycle_status) VALUES ($1,$2,$3,$4,'MANUFACTURED')`,
		id, vin, model, claimCode)
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, id)
}

func (s *Store) GetByID(ctx context.Context, id string) (*Vehicle, error) {
	return scanVehicle(s.pool.QueryRow(ctx, `SELECT `+vehicleCols+` FROM vehicles WHERE id=$1`, id))
}

func (s *Store) GetByVIN(ctx context.Context, vin string) (*Vehicle, error) {
	return scanVehicle(s.pool.QueryRow(ctx, `SELECT `+vehicleCols+` FROM vehicles WHERE vin=$1`, vin))
}

func (s *Store) ListAll(ctx context.Context) ([]Vehicle, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+vehicleCols+` FROM vehicles ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectVehicles(rows)
}

func (s *Store) ListForUser(ctx context.Context, username string) ([]Vehicle, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+prefixCols("v")+` FROM vehicles v
		   JOIN vehicle_grants g ON g.vehicle_id = v.id
		  WHERE g.username=$1 ORDER BY v.created_at DESC`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectVehicles(rows)
}

func collectVehicles(rows pgx.Rows) ([]Vehicle, error) {
	out := []Vehicle{}
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func prefixCols(alias string) string {
	return alias + ".id, " + alias + ".vin, " + alias + ".model, " + alias + ".claim_code, " +
		alias + ".lifecycle_status, " + alias + ".access_state, " + alias + ".power_state, " +
		alias + ".climate_state, " + alias + ".connectivity_state, " + alias + ".ownership_state, " +
		alias + ".created_at, " + alias + ".last_heartbeat_at"
}

// RegisterDevice records the device identity and advances lifecycle to CLAIMABLE.
func (s *Store) RegisterDevice(ctx context.Context, vehicleID, subject string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO vehicle_device_identities (vehicle_id, subject, last_seen_at)
		 VALUES ($1,$2,now())
		 ON CONFLICT (vehicle_id) DO UPDATE SET subject=EXCLUDED.subject, last_seen_at=now()`,
		vehicleID, subject); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vehicles SET lifecycle_status='CLAIMABLE', connectivity_state='ONLINE',
		        last_heartbeat_at=now() WHERE id=$1`, vehicleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Heartbeat(ctx context.Context, vehicleID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE vehicles SET connectivity_state='ONLINE', last_heartbeat_at=now() WHERE id=$1`, vehicleID)
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE vehicle_device_identities SET last_seen_at=now() WHERE vehicle_id=$1`, vehicleID)
	}
	return err
}

func (s *Store) SetAccessState(ctx context.Context, id, state string) error {
	_, err := s.pool.Exec(ctx, `UPDATE vehicles SET access_state=$2 WHERE id=$1`, id, state)
	return err
}

func (s *Store) SetClimateState(ctx context.Context, id, state string) error {
	_, err := s.pool.Exec(ctx, `UPDATE vehicles SET climate_state=$2 WHERE id=$1`, id, state)
	return err
}

func (s *Store) SetPowerState(ctx context.Context, id, state string) error {
	_, err := s.pool.Exec(ctx, `UPDATE vehicles SET power_state=$2 WHERE id=$1`, id, state)
	return err
}

// Claim assigns the first owner and moves the vehicle to CLAIMED / OWNER_ASSIGNED.
func (s *Store) Claim(ctx context.Context, v *Vehicle, userID, username string) error {
	return s.assignOwnerTx(ctx, v.ID, userID, username)
}

// AssignOwner is the staff (sales_support) override that assigns an owner without a claim code.
func (s *Store) AssignOwner(ctx context.Context, vehicleID, username string) error {
	return s.assignOwnerTx(ctx, vehicleID, "", username)
}

func (s *Store) assignOwnerTx(ctx context.Context, vehicleID, userID, username string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO vehicle_grants (id, vehicle_id, user_id, username, role)
		 VALUES ($1,$2,$3,$4,'owner')
		 ON CONFLICT (vehicle_id, username) DO UPDATE SET role='owner', user_id=EXCLUDED.user_id`,
		uuid.NewString(), vehicleID, userID, username); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vehicles SET lifecycle_status='CLAIMED', ownership_state='OWNER_ASSIGNED' WHERE id=$1`, vehicleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GrantRole(ctx context.Context, vehicleID, userID, username, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vehicle_grants (id, vehicle_id, user_id, username, role)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (vehicle_id, username) DO UPDATE SET role=EXCLUDED.role, user_id=EXCLUDED.user_id`,
		uuid.NewString(), vehicleID, userID, username, role)
	return err
}

// GetRole resolves a consumer's role on a vehicle by username (the stable principal key).
func (s *Store) GetRole(ctx context.Context, vehicleID, username string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM vehicle_grants WHERE vehicle_id=$1 AND username=$2`, vehicleID, username).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// Grant is a role assignment on a vehicle.
type Grant struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *Store) ListGrants(ctx context.Context, vehicleID string) ([]Grant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, username, role FROM vehicle_grants WHERE vehicle_id=$1 ORDER BY granted_at`, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.UserID, &g.Username, &g.Role); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Invitation is a pending or accepted driver invitation.
type Invitation struct {
	Code            string    `json:"code"`
	VehicleID       string    `json:"vehicle_id"`
	VIN             string    `json:"vin"`
	InvitedUsername string    `json:"invited_username"`
	Role            string    `json:"role"`
	Status          string    `json:"status"`
	InvitedBy       string    `json:"invited_by"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) CreateInvitation(ctx context.Context, vehicleID, username, role, invitedBy, code string) (*Invitation, error) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vehicle_invitations (code, vehicle_id, invited_username, role, invited_by)
		 VALUES ($1,$2,$3,$4,$5)`,
		code, vehicleID, username, role, invitedBy)
	if err != nil {
		return nil, err
	}
	return s.GetInvitation(ctx, code)
}

func (s *Store) GetInvitation(ctx context.Context, code string) (*Invitation, error) {
	var inv Invitation
	err := s.pool.QueryRow(ctx,
		`SELECT i.code, i.vehicle_id, v.vin, i.invited_username, i.role, i.status, i.invited_by, i.created_at
		   FROM vehicle_invitations i JOIN vehicles v ON v.id = i.vehicle_id
		  WHERE i.code=$1`, code).
		Scan(&inv.Code, &inv.VehicleID, &inv.VIN, &inv.InvitedUsername, &inv.Role, &inv.Status, &inv.InvitedBy, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &inv, err
}

func (s *Store) ListInvitationsForUsername(ctx context.Context, username, status string) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT i.code, i.vehicle_id, v.vin, i.invited_username, i.role, i.status, i.invited_by, i.created_at
		   FROM vehicle_invitations i JOIN vehicles v ON v.id = i.vehicle_id
		  WHERE i.invited_username=$1 AND i.status=$2 ORDER BY i.created_at DESC`, username, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.Code, &inv.VehicleID, &inv.VIN, &inv.InvitedUsername, &inv.Role,
			&inv.Status, &inv.InvitedBy, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// AcceptInvitation marks an invitation accepted and grants the role in one transaction.
func (s *Store) AcceptInvitation(ctx context.Context, inv *Invitation, userID, username string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE vehicle_invitations SET status='accepted', accepted_at=now() WHERE code=$1`, inv.Code); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO vehicle_grants (id, vehicle_id, user_id, username, role)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (vehicle_id, username) DO UPDATE SET role=EXCLUDED.role, user_id=EXCLUDED.user_id`,
		uuid.NewString(), inv.VehicleID, userID, username, inv.Role); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FindCommand returns the decision recorded for a prior command with the same
// idempotency key, if any (used to make high-risk commands idempotent).
func (s *Store) FindCommand(ctx context.Context, vehicleID, command, idempotencyKey string) (string, bool, error) {
	if idempotencyKey == "" {
		return "", false, nil
	}
	var decision string
	err := s.pool.QueryRow(ctx,
		`SELECT decision FROM vehicle_commands WHERE vehicle_id=$1 AND command=$2 AND idempotency_key=$3`,
		vehicleID, command, idempotencyKey).Scan(&decision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return decision, true, nil
}

// InsertCommand persists a command attempt (allowed or denied) for the audit trail
// and idempotency.
func (s *Store) InsertCommand(ctx context.Context, vehicleID, command, idempotencyKey, actorID, decision, reason string) error {
	var key any
	if idempotencyKey != "" {
		key = idempotencyKey
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vehicle_commands (id, vehicle_id, command, idempotency_key, actor_id, decision, reason)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uuid.NewString(), vehicleID, command, key, actorID, decision, reason)
	return err
}
