package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"vehicle-identity-demo/packages/clients/audit"
	identityclient "vehicle-identity-demo/packages/clients/identity"
	"vehicle-identity-demo/packages/shared/db"
	"vehicle-identity-demo/packages/shared/httpx"
	sjwt "vehicle-identity-demo/packages/shared/jwt"
	"vehicle-identity-demo/packages/shared/middleware"
	"vehicle-identity-demo/packages/shared/models"
)

//go:embed schema.sql
var schema string

func main() {
	ctx := context.Background()

	pool, err := db.Connect(ctx, env("VEHICLE_DB", "vehicle"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, schema); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	identityURL := env("IDENTITY_URL", "http://identity-service:8081")
	auditURL := env("AUDIT_URL", "http://audit-service:8083")

	// vehicle-service writes audit events using a cached audit.write token obtained
	// from identity-service with its own workload credentials.
	idClient := identityclient.New(identityURL)
	clientID := env("VEHICLE_SERVICE_CLIENT_ID", "vehicle-service")
	clientSecret := env("VEHICLE_SERVICE_CLIENT_SECRET", "vehicle-service-secret")
	auditToken := identityclient.NewCachedToken(func(c context.Context) (identityclient.Token, error) {
		return idClient.ServiceToken(c, clientID, clientSecret, models.AudAuditService, models.ScopeAuditWrite)
	})

	app := &App{
		store:    &Store{pool: pool},
		identity: idClient,
		audit:    audit.New(auditURL, auditToken.Value),
	}

	verifier := sjwt.NewVerifierForIdentity(identityURL, env("JWT_ISSUER", "vehicle-demo.identity-service"))
	requireRegister := middleware.RequireScope(verifier, models.AudVehicleService, models.ScopeVehicleRegister)
	requireHeartbeat := middleware.RequireScope(verifier, models.AudVehicleService, models.ScopeVehicleHeartbeat)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /staff/vehicles/create", app.handleCreate)
	mux.HandleFunc("POST /staff/vehicles/{id}/assign-owner", app.handleAssignOwner)
	mux.HandleFunc("GET /vehicles", app.handleListVehicles)
	mux.HandleFunc("GET /vehicles/lookup", app.handleLookup)
	mux.HandleFunc("GET /vehicles/{id}", app.handleGetVehicle)
	mux.HandleFunc("POST /vehicles/{id}/claim", app.handleClaim)
	mux.HandleFunc("POST /vehicles/{id}/invite", app.handleInvite)
	mux.HandleFunc("GET /invitations", app.handleListInvitations)
	mux.HandleFunc("POST /invitations/{code}/accept", app.handleAcceptInvite)
	mux.HandleFunc("POST /vehicles/{id}/commands/unlock", app.handleUnlock)
	mux.HandleFunc("POST /vehicles/{id}/commands/start-climate", app.handleStartClimate)
	mux.HandleFunc("POST /vehicles/{id}/commands/start-vehicle", app.handleStartVehicle)
	mux.Handle("POST /vehicles/register", requireRegister(http.HandlerFunc(app.handleRegister)))
	mux.Handle("POST /vehicles/{id}/heartbeat", requireHeartbeat(http.HandlerFunc(app.handleHeartbeat)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	origins := splitCSV(env("WEB_ORIGINS", "http://localhost:5173,http://localhost:5174"))
	handler := middleware.Correlation(middleware.CORS(origins...)(mux))

	addr := ":" + env("PORT", "8082")
	log.Printf("vehicle-service listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
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
