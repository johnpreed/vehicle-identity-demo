package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps Postgres access for the identity-service.
type Store struct{ pool *pgxpool.Pool }

// User is a consumer account.
type User struct {
	ID          string
	Username    string
	DisplayName string
}

// Session is a consumer web session.
type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
	StepUpAt  *time.Time
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, display_name FROM users WHERE username=$1`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, display_name FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

func (s *Store) CreateUser(ctx context.Context, username string) (*User, error) {
	u := &User{ID: uuid.NewString(), Username: username, DisplayName: username}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, username, display_name) VALUES ($1,$2,$3)`,
		u.ID, u.Username, u.DisplayName)
	return u, err
}

// CreateUserWithID persists a user using an id chosen at the start of registration,
// so the user row is only created once the passkey ceremony succeeds.
func (s *Store) CreateUserWithID(ctx context.Context, id, username string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, username, display_name) VALUES ($1,$2,$3)
		 ON CONFLICT (id) DO NOTHING`,
		id, username, username)
	return err
}

func (s *Store) Credentials(ctx context.Context, userID string) ([]webauthn.Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT credential_json FROM passkey_credentials WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var creds []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (s *Store) AddCredential(ctx context.Context, userID, credID string, cred *webauthn.Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO passkey_credentials (id, user_id, credential_json) VALUES ($1,$2,$3)
		 ON CONFLICT (id) DO UPDATE SET credential_json=EXCLUDED.credential_json`,
		credID, userID, raw)
	return err
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (*Session, error) {
	sess := &Session{
		Token:     uuid.NewString() + uuid.NewString(),
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl),
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES ($1,$2,$3)`,
		sess.Token, sess.UserID, sess.ExpiresAt)
	return sess, err
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx,
		`SELECT token, user_id, expires_at, step_up_at FROM sessions WHERE token=$1`, token).
		Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.StepUpAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return nil, nil
	}
	return &sess, nil
}

func (s *Store) MarkStepUp(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET step_up_at=now() WHERE token=$1`, token)
	return err
}

func (s *Store) GetServiceIdentity(ctx context.Context, clientID string) (subject, secret, scopes, auds string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT subject, client_secret, allowed_scopes, allowed_audiences
		   FROM service_identities WHERE client_id=$1`, clientID).
		Scan(&subject, &secret, &scopes, &auds)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", "", nil
	}
	return
}

func (s *Store) GetBootstrapSecret(ctx context.Context, vin string) (string, error) {
	var secret string
	err := s.pool.QueryRow(ctx,
		`SELECT bootstrap_secret FROM vehicle_bootstrap_credentials WHERE vin=$1`, vin).
		Scan(&secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return secret, err
}

// UpsertServiceIdentity is used for self-seeding required workload credentials.
func (s *Store) UpsertServiceIdentity(ctx context.Context, clientID, secret, subject, scopes, auds string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO service_identities (client_id, client_secret, subject, allowed_scopes, allowed_audiences)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (client_id) DO UPDATE SET
		   client_secret=EXCLUDED.client_secret, subject=EXCLUDED.subject,
		   allowed_scopes=EXCLUDED.allowed_scopes, allowed_audiences=EXCLUDED.allowed_audiences`,
		clientID, secret, subject, scopes, auds)
	return err
}

// UpsertBootstrapCredential self-seeds the factory-provisioned device secret.
func (s *Store) UpsertBootstrapCredential(ctx context.Context, vin, secret string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO vehicle_bootstrap_credentials (vin, bootstrap_secret) VALUES ($1,$2)
		 ON CONFLICT (vin) DO UPDATE SET bootstrap_secret=EXCLUDED.bootstrap_secret`,
		vin, secret)
	return err
}
