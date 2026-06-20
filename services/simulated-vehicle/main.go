// Command simulated-vehicle is a fleet simulator: it models the physical devices for
// every manufactured vehicle. It discovers spawned vehicles from vehicle-service,
// performs factory "burn-in" (provisioning a bootstrap credential at identity-service
// using a scoped factory workload token), then exchanges each device's VIN +
// bootstrap secret for a short-lived JWT, registers the device, and heartbeats.
//
// The seeded device (SIM_VIN) keeps its pre-provisioned env secret; every other VIN
// the simulator discovers is burned in on the fly.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"vehicle-identity-demo/packages/shared/httpx"
)

// device is the simulator's per-VIN state.
type device struct {
	vin        string
	secret     string
	vehicleID  string
	token      string
	tokenExp   time.Time
	registered bool
	lastBeat   time.Time
	lastError  string
}

type fleet struct {
	identityURL   string
	vehicleURL    string
	seedVIN       string
	seedSecret    string
	factoryID     string
	factorySecret string
	interval      time.Duration

	mu      sync.Mutex
	devices map[string]*device
}

func main() {
	f := &fleet{
		identityURL:   env("IDENTITY_URL", "http://identity-service:8081"),
		vehicleURL:    env("VEHICLE_URL", "http://vehicle-service:8082"),
		seedVIN:       env("SIM_VIN", "VIN-DEMO-0001"),
		seedSecret:    env("SIM_BOOTSTRAP_SECRET", "bootstrap-demo-secret"),
		factoryID:     env("FACTORY_CLIENT_ID", "vehicle-factory"),
		factorySecret: env("FACTORY_CLIENT_SECRET", "vehicle-factory-secret"),
		interval:      durationEnv("RECONCILE_INTERVAL", 8*time.Second),
		devices:       map[string]*device{},
	}

	go f.run()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", f.handleStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	addr := ":" + env("PORT", "8084")
	log.Printf("simulated-vehicle fleet listening on %s", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func (f *fleet) run() {
	ctx := context.Background()
	for {
		f.reconcile(ctx)
		time.Sleep(f.interval)
	}
}

// reconcile discovers all spawned vehicles, brings new ones online, and heartbeats
// the rest. Discovery uses the manufacturing persona: the simulator is the
// manufacturer's device fleet gateway.
func (f *fleet) reconcile(ctx context.Context) {
	vehicles, err := f.listVehicles(ctx)
	if err != nil {
		log.Printf("[fleet] discovery error: %v", err)
		return
	}
	for _, v := range vehicles {
		f.mu.Lock()
		d, known := f.devices[v.VIN]
		if !known {
			d = &device{vin: v.VIN}
			f.devices[v.VIN] = d
		}
		f.mu.Unlock()

		if !d.registered {
			f.bringOnline(ctx, d)
		} else {
			f.heartbeat(ctx, d)
		}
	}
}

// bringOnline burns in a device credential (if needed) and registers the device.
func (f *fleet) bringOnline(ctx context.Context, d *device) {
	if d.secret == "" {
		if d.vin == f.seedVIN && f.seedSecret != "" {
			d.secret = f.seedSecret // pre-provisioned seeded device
		} else {
			secret := newSecret()
			if err := f.provision(ctx, d.vin, secret); err != nil {
				d.lastError = "provision: " + err.Error()
				log.Printf("[fleet] %s provision error: %v", d.vin, err)
				return
			}
			d.secret = secret
			log.Printf("[fleet] %s burned in (factory provisioning)", d.vin)
		}
	}
	token, err := f.deviceToken(ctx, d)
	if err != nil {
		d.lastError = "bootstrap token: " + err.Error()
		return
	}
	var out struct {
		ID string `json:"id"`
	}
	if _, err := httpx.PostJSON(ctx, f.vehicleURL+"/vehicles/register", token, "sim-"+d.vin,
		map[string]string{"vin": d.vin}, &out); err != nil {
		d.lastError = "register: " + err.Error()
		log.Printf("[fleet] %s register not ready: %v", d.vin, err)
		return
	}
	d.vehicleID = out.ID
	d.registered = true
	d.lastError = ""
	log.Printf("[fleet] %s registered as %s", d.vin, d.vehicleID)
}

func (f *fleet) heartbeat(ctx context.Context, d *device) {
	token, err := f.deviceToken(ctx, d)
	if err != nil {
		d.lastError = "heartbeat token: " + err.Error()
		return
	}
	if _, err := httpx.PostJSON(ctx, f.vehicleURL+"/vehicles/"+d.vehicleID+"/heartbeat", token, "sim-"+d.vin, nil, nil); err != nil {
		d.lastError = "heartbeat: " + err.Error()
		return
	}
	d.lastBeat = time.Now()
	d.lastError = ""
}

// ---- identity-service interactions ----

// deviceToken exchanges the device's VIN + bootstrap secret for a short-lived JWT.
func (f *fleet) deviceToken(ctx context.Context, d *device) (string, error) {
	if d.token != "" && time.Now().Before(d.tokenExp.Add(-1*time.Minute)) {
		return d.token, nil
	}
	var out struct {
		Token string `json:"token"`
	}
	body := map[string]string{
		"grant_type":       "vehicle_bootstrap",
		"vin":              d.vin,
		"bootstrap_secret": d.secret,
		"audience":         "vehicle-service",
	}
	if _, err := httpx.PostJSON(ctx, f.identityURL+"/service-token", "", "sim-"+d.vin, body, &out); err != nil {
		return "", err
	}
	d.token = out.Token
	d.tokenExp = time.Now().Add(5 * time.Minute)
	return d.token, nil
}

// provision burns in a bootstrap credential using a factory workload token.
func (f *fleet) provision(ctx context.Context, vin, secret string) error {
	token, err := f.factoryToken(ctx)
	if err != nil {
		return err
	}
	_, err = httpx.PostJSON(ctx, f.identityURL+"/bootstrap/provision", token, "sim-"+vin,
		map[string]string{"vin": vin, "bootstrap_secret": secret}, nil)
	return err
}

func (f *fleet) factoryToken(ctx context.Context) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	body := map[string]string{
		"grant_type":    "service_credential",
		"client_id":     f.factoryID,
		"client_secret": f.factorySecret,
		"audience":      "identity-service",
		"scope":         "bootstrap.provision",
	}
	if _, err := httpx.PostJSON(ctx, f.identityURL+"/service-token", "", "sim-factory", body, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// ---- vehicle-service interactions ----

type vehicleSummary struct {
	VIN string `json:"vin"`
}

// listVehicles fetches the fleet from vehicle-service using the manufacturing persona.
func (f *fleet) listVehicles(ctx context.Context) ([]vehicleSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.vehicleURL+"/vehicles", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Staff-Persona", "manufacturing")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list vehicles returned %d", resp.StatusCode)
	}
	var body struct {
		Vehicles []vehicleSummary `json:"vehicles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Vehicles, nil
}

func (f *fleet) handleStatus(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	vins := make([]string, 0, len(f.devices))
	for vin := range f.devices {
		vins = append(vins, vin)
	}
	sort.Strings(vins)
	out := make([]map[string]any, 0, len(vins))
	registered := 0
	for _, vin := range vins {
		d := f.devices[vin]
		if d.registered {
			registered++
		}
		out = append(out, map[string]any{
			"vin":            d.vin,
			"registered":     d.registered,
			"vehicle_id":     d.vehicleID,
			"last_heartbeat": d.lastBeat,
			"last_error":     d.lastError,
		})
	}
	f.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"fleet_size": len(out),
		"registered": registered,
		"devices":    out,
	})
}

func newSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "bootstrap-" + hex.EncodeToString(b)
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
