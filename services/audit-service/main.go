package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"vehicle-identity-demo/packages/shared/db"
	"vehicle-identity-demo/packages/shared/httpx"
	sjwt "vehicle-identity-demo/packages/shared/jwt"
	"vehicle-identity-demo/packages/shared/middleware"
	"vehicle-identity-demo/packages/shared/models"
)

//go:embed schema.sql
var schema string

type App struct{ store *Store }

func main() {
	ctx := context.Background()

	pool, err := db.Connect(ctx, env("AUDIT_DB", "audit"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, schema); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	app := &App{store: &Store{pool: pool}}

	jwksURL := env("IDENTITY_URL", "http://identity-service:8081") + "/.well-known/jwks.json"
	verifier := sjwt.NewVerifier(jwksURL, env("JWT_ISSUER", "vehicle-demo.identity-service"))
	requireWrite := middleware.RequireScope(verifier, models.AudAuditService, models.ScopeAuditWrite)

	mux := http.NewServeMux()
	// Writes require a short-lived JWT with the audit.write scope.
	mux.Handle("POST /audit", requireWrite(http.HandlerFunc(app.handleWrite)))
	// Search is restricted to the staff security_auditor persona.
	mux.HandleFunc("GET /audit/search", app.handleSearch)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	origins := splitCSV(env("WEB_ORIGINS", "http://localhost:5173,http://localhost:5174"))
	handler := middleware.Correlation(middleware.CORS(origins...)(mux))

	addr := ":" + env("PORT", "8083")
	log.Printf("audit-service listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (a *App) handleWrite(w http.ResponseWriter, r *http.Request) {
	var e models.AuditEvent
	if err := httpx.ReadJSON(r, &e); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid audit event")
		return
	}
	if e.CorrelationID == "" {
		e.CorrelationID = httpx.CorrelationID(r.Context())
	}
	// Record which workload wrote the event (from the verified JWT subject).
	if claims := middleware.ClaimsFrom(r.Context()); claims != nil {
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		e.Metadata["writer_sub"] = claims.Subject
	}
	id, err := a.store.Insert(r.Context(), e)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Staff-Persona") != models.PersonaSecurityAuditor {
		httpx.WriteError(w, http.StatusForbidden, "read_audit_logs requires security_auditor persona")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	records, err := a.store.Search(r.Context(), SearchFilters{
		ResourceType:  q.Get("resource_type"),
		ResourceID:    q.Get("resource_id"),
		ActorID:       q.Get("actor_id"),
		Action:        q.Get("action"),
		Decision:      q.Get("decision"),
		CorrelationID: q.Get("correlation_id"),
		Limit:         limit,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": records})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
