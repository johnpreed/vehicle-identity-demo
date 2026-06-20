package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"vehicle-identity-demo/packages/shared/models"
)

// Store wraps Postgres access for the audit-service.
type Store struct{ pool *pgxpool.Pool }

// Record is a stored audit event returned by search.
type Record struct {
	ID            string         `json:"id"`
	CorrelationID string         `json:"correlation_id"`
	ActorType     string         `json:"actor_type"`
	ActorID       string         `json:"actor_id"`
	Action        string         `json:"action"`
	ResourceType  string         `json:"resource_type"`
	ResourceID    string         `json:"resource_id"`
	Decision      string         `json:"decision"`
	Reason        string         `json:"reason"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"created_at"`
}

func (s *Store) Insert(ctx context.Context, e models.AuditEvent) (string, error) {
	id := uuid.NewString()
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO audit_logs
		   (id, correlation_id, actor_type, actor_id, action, resource_type, resource_id, decision, reason, metadata_json)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, e.CorrelationID, e.ActorType, e.ActorID, e.Action,
		e.ResourceType, e.ResourceID, e.Decision, e.Reason, raw)
	return id, err
}

// SearchFilters holds optional equality filters for audit search.
type SearchFilters struct {
	ResourceType  string
	ResourceID    string
	ActorID       string
	Action        string
	Decision      string
	CorrelationID string
	Limit         int
}

func (s *Store) Search(ctx context.Context, f SearchFilters) ([]Record, error) {
	query := `SELECT id, correlation_id, actor_type, actor_id, action, resource_type,
	                 resource_id, decision, reason, metadata_json, created_at
	            FROM audit_logs WHERE 1=1`
	var args []any
	add := func(col, val string) {
		if val != "" {
			args = append(args, val)
			query += fmt.Sprintf(" AND %s=$%d", col, len(args))
		}
	}
	// resource_id (vehicle id) matches as a case-insensitive substring so staff can
	// filter by a full or partial id.
	addLike := func(col, val string) {
		if val != "" {
			args = append(args, "%"+val+"%")
			query += fmt.Sprintf(" AND %s ILIKE $%d", col, len(args))
		}
	}
	add("resource_type", f.ResourceType)
	addLike("resource_id", f.ResourceID)
	add("actor_id", f.ActorID)
	add("action", f.Action)
	add("decision", f.Decision)
	add("correlation_id", f.CorrelationID)

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, strconv.Itoa(limit))
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Record{}
	for rows.Next() {
		var r Record
		var raw []byte
		if err := rows.Scan(&r.ID, &r.CorrelationID, &r.ActorType, &r.ActorID, &r.Action,
			&r.ResourceType, &r.ResourceID, &r.Decision, &r.Reason, &raw, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &r.Metadata)
		out = append(out, r)
	}
	return out, rows.Err()
}
