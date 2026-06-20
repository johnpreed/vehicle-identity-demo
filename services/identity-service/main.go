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

	"vehicle-identity-demo/packages/shared/db"
	"vehicle-identity-demo/packages/shared/httpx"
	sjwt "vehicle-identity-demo/packages/shared/jwt"
	"vehicle-identity-demo/packages/shared/middleware"
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

	app := &App{store: store, web: web, ceremony: newCeremonyStore(), issuer: issuer}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup/start", app.handleSignupStart)
	mux.HandleFunc("POST /signup/finish", app.handleSignupFinish)
	mux.HandleFunc("POST /signin/start", app.handleSigninStart)
	mux.HandleFunc("POST /signin/finish", app.handleSigninFinish)
	mux.HandleFunc("POST /step-up/start", app.handleStepUpStart)
	mux.HandleFunc("POST /step-up/finish", app.handleStepUpFinish)
	mux.HandleFunc("POST /service-token", app.handleServiceToken)
	mux.HandleFunc("GET /.well-known/jwks.json", app.handleJWKS)
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

// selfSeed provisions the credentials the platform needs to function out of the box:
// the simulated vehicle's factory bootstrap secret and the vehicle-service workload client.
func selfSeed(ctx context.Context, store *Store) error {
	if err := store.UpsertBootstrapCredential(ctx,
		env("SIM_VIN", "VIN-DEMO-0001"),
		env("SIM_BOOTSTRAP_SECRET", "bootstrap-demo-secret"),
	); err != nil {
		return err
	}
	return store.UpsertServiceIdentity(ctx,
		env("VEHICLE_SERVICE_CLIENT_ID", "vehicle-service"),
		env("VEHICLE_SERVICE_CLIENT_SECRET", "vehicle-service-secret"),
		"service:vehicle-service",
		"audit.write audit.read",
		"audit-service",
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
