// Command simulated-vehicle models a vehicle's firmware. On boot it exchanges its
// factory-provisioned VIN + bootstrap secret with identity-service for a short-lived
// workload JWT, registers with vehicle-service, then sends periodic heartbeats.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
)

type vehicle struct {
	vin         string
	secret      string
	identityURL string
	vehicleURL  string
	hbInterval  time.Duration
	correlation string

	mu        sync.Mutex
	token     string
	tokenExp  time.Time
	vehicleID string
	regd      bool
	lastBeat  time.Time
	lastError string
}

func main() {
	v := &vehicle{
		vin:         env("SIM_VIN", "VIN-DEMO-0001"),
		secret:      env("SIM_BOOTSTRAP_SECRET", "bootstrap-demo-secret"),
		identityURL: env("IDENTITY_URL", "http://identity-service:8081"),
		vehicleURL:  env("VEHICLE_URL", "http://vehicle-service:8082"),
		hbInterval:  durationEnv("HEARTBEAT_INTERVAL", 15*time.Second),
		correlation: "sim-" + env("SIM_VIN", "VIN-DEMO-0001"),
	}

	go v.run()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", v.handleStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	addr := ":" + env("PORT", "8084")
	log.Printf("simulated-vehicle %s listening on %s", v.vin, addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (v *vehicle) run() {
	ctx := context.Background()
	log.Printf("[%s] calling home...", v.vin)
	for !v.tryRegister(ctx) {
		time.Sleep(5 * time.Second)
	}
	log.Printf("[%s] registered as vehicle %s", v.vin, v.vehicleID)
	for {
		time.Sleep(v.hbInterval)
		v.heartbeat(ctx)
	}
}

func (v *vehicle) tryRegister(ctx context.Context) bool {
	token, err := v.tokenFor(ctx)
	if err != nil {
		v.setError("bootstrap token: " + err.Error())
		log.Printf("[%s] bootstrap token error: %v", v.vin, err)
		return false
	}
	var out struct {
		ID              string `json:"id"`
		LifecycleStatus string `json:"lifecycle_status"`
	}
	status, err := httpx.PostJSON(ctx, v.vehicleURL+"/vehicles/register", token, v.correlation,
		map[string]string{"vin": v.vin}, &out)
	if err != nil {
		v.setError("register: " + err.Error())
		log.Printf("[%s] register not ready (%d): %v", v.vin, status, err)
		return false
	}
	v.mu.Lock()
	v.vehicleID = out.ID
	v.regd = true
	v.lastError = ""
	v.mu.Unlock()
	return true
}

func (v *vehicle) heartbeat(ctx context.Context) {
	token, err := v.tokenFor(ctx)
	if err != nil {
		v.setError("heartbeat token: " + err.Error())
		return
	}
	v.mu.Lock()
	id := v.vehicleID
	v.mu.Unlock()
	if _, err := httpx.PostJSON(ctx, v.vehicleURL+"/vehicles/"+id+"/heartbeat", token, v.correlation, nil, nil); err != nil {
		v.setError("heartbeat: " + err.Error())
		log.Printf("[%s] heartbeat error: %v", v.vin, err)
		return
	}
	v.mu.Lock()
	v.lastBeat = time.Now()
	v.lastError = ""
	v.mu.Unlock()
}

// tokenFor returns a cached bootstrap token, refreshing shortly before expiry.
func (v *vehicle) tokenFor(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.token != "" && time.Now().Before(v.tokenExp.Add(-1*time.Minute)) {
		return v.token, nil
	}
	var out struct {
		Token string `json:"token"`
	}
	body := map[string]string{
		"grant_type":       "vehicle_bootstrap",
		"vin":              v.vin,
		"bootstrap_secret": v.secret,
		"audience":         "vehicle-service",
	}
	if _, err := httpx.PostJSON(ctx, v.identityURL+"/service-token", "", v.correlation, body, &out); err != nil {
		return "", err
	}
	v.token = out.Token
	v.tokenExp = time.Now().Add(5 * time.Minute)
	return v.token, nil
}

func (v *vehicle) setError(msg string) {
	v.mu.Lock()
	v.lastError = msg
	v.mu.Unlock()
}

func (v *vehicle) handleStatus(w http.ResponseWriter, r *http.Request) {
	v.mu.Lock()
	defer v.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"vin":            v.vin,
		"registered":     v.regd,
		"vehicle_id":     v.vehicleID,
		"last_heartbeat": v.lastBeat,
		"last_error":     v.lastError,
	})
}

func env(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

func durationEnv(key string, def time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return def
}
