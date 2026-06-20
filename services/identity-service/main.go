package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	auditclient "vehicle-identity-demo/packages/clients/audit"
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

	pool, err := db.Connect(ctx, env("IDENTITY_DB", "identity"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, schema); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	store := &Store{pool: pool}

	wconfig := &webauthn.Config{
		RPID:          env("WEBAUTHN_RP_ID", "localhost"),
		RPDisplayName: env("WEBAUTHN_RP_NAME", "Vehicle Identity Demo"),
		RPOrigins:     splitCSV(env("WEBAUTHN_RP_ORIGINS", "http://localhost:5173")),
	}
	web, err := webauthn.New(wconfig)
	if err != nil {
		log.Fatalf("webauthn: %v", err)
	}

	issuer, err := sjwt.NewIssuer(env("JWT_ISSUER", "vehicle-demo.identity-service"))
	if err != nil {
		log.Fatalf("issuer: %v", err)
	}

	if err := selfSeed(ctx, store); err != nil {
		log.Fatalf("seed: %v", err)
	}

	// identity-service writes audit events using its own signing key: it self-issues
	// the audit.write token directly (no HTTP, no recursion into service_token_issued).
	auditClient := auditclient.New(env("AUDIT_URL", "http://audit-service:8083"),
		func(c context.Context) (string, error) {
			return issuer.Issue("service:identity-service", models.AudAuditService, models.ScopeAuditWrite)
		})
	app := &App{store: store, web: web, ceremony: newCeremonyStore(), issuer: issuer, audit: auditClient}

	// Record the signing key's "birth" so key lifecycle is visible in the audit log.
	// A restart generates a new key and emits a fresh event (a key roll). This runs
	// in the background and retries because audit-service may still be starting.
	go auditClient.EmitWithRetry(context.Background(), httpx.NewCorrelationID(), models.AuditEvent{
		ActorType:    models.ActorService,
		ActorID:      "service:identity-service",
		Action:       "signing_key_generated",
		ResourceType: "signing_key",
		ResourceID:   issuer.KeyID(),
		Decision:     models.DecisionAllow,
		Reason:       "Ed25519 signing key generated at startup",
		Metadata:     map[string]any{"alg": "EdDSA", "use": "sig", "kid": issuer.KeyID()},
	})

	// identity-service verifies its own tokens (issued for audience identity-service)
	// locally from the issuer's public key, so the factory provisioning endpoint can
	// be JWT-protected without a self-directed JWKS HTTP call.
	verifier := sjwt.NewStaticVerifier(env("JWT_ISSUER", "vehicle-demo.identity-service"), issuer.PublicKeys())
	requireProvision := middleware.RequireScope(verifier, models.AudIdentityService, models.ScopeBootstrapProvision)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup/start", app.handleSignupStart)
	mux.HandleFunc("POST /signup/finish", app.handleSignupFinish)
	mux.HandleFunc("POST /signin/start", app.handleSigninStart)
	mux.HandleFunc("POST /signin/finish", app.handleSigninFinish)
	mux.HandleFunc("POST /step-up/start", app.handleStepUpStart)
	mux.HandleFunc("POST /step-up/finish", app.handleStepUpFinish)
	mux.HandleFunc("POST /service-token", app.handleServiceToken)
	mux.Handle("POST /bootstrap/provision", requireProvision(http.HandlerFunc(app.handleProvisionBootstrap)))
	mux.HandleFunc("GET "+sjwt.JWKSPath, app.handleJWKS)
	mux.HandleFunc("GET /me", app.handleMe)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	origins := splitCSV(env("WEB_ORIGINS", "http://localhost:5173,http://localhost:5174"))
	handler := middleware.Correlation(middleware.CORS(origins...)(mux))

	addr := ":" + env("PORT", "8081")
	log.Printf("identity-service listening on %s (rp_id=%s)", addr, wconfig.RPID)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// selfSeed provisions the workload credentials the platform needs to function out of
// the box: the vehicle-service client (audit writes) and the vehicle-factory client
// (bootstrap provisioning). Vehicle bootstrap credentials are not pre-seeded — the
// factory provisions them on demand as vehicles are created.
func selfSeed(ctx context.Context, store *Store) error {
	if err := store.UpsertServiceIdentity(ctx,
		env("VEHICLE_SERVICE_CLIENT_ID", "vehicle-service"),
		env("VEHICLE_SERVICE_CLIENT_SECRET", "vehicle-service-secret"),
		"service:vehicle-service",
		"audit.write audit.read",
		"audit-service",
	); err != nil {
		return err
	}
	// The vehicle factory workload mints bootstrap.provision tokens (audience
	// identity-service) so the fleet simulator can burn in device credentials.
	return store.UpsertServiceIdentity(ctx,
		env("FACTORY_CLIENT_ID", "vehicle-factory"),
		env("FACTORY_CLIENT_SECRET", "vehicle-factory-secret"),
		"service:vehicle-factory",
		"bootstrap.provision",
		"identity-service",
	)
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
