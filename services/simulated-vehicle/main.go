// Command simulated-vehicle is a fleet simulator: it models the physical devices for
// every manufactured vehicle. It discovers created vehicles from vehicle-service,
// performs factory "burn-in" (provisioning a bootstrap credential at identity-service
// using a scoped factory workload token), then exchanges each device's VIN +
// bootstrap secret for a short-lived JWT, registers the device, and heartbeats.
//
// The fleet starts empty and treats every discovered VIN uniformly. All inter-service
// calls go through the shared identity/vehicle client libraries.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	identityclient "vehicle-identity-demo/packages/clients/identity"
	vehicleclient "vehicle-identity-demo/packages/clients/vehicle"
	"vehicle-identity-demo/packages/shared/httpx"
	"vehicle-identity-demo/packages/shared/models"
)

// device is the simulator's per-VIN state.
type device struct {
	vin        string
	secret     string
	vehicleID  string
	token      *identityclient.CachedToken // per-device bootstrap token
	registered bool
	lastBeat   time.Time
	lastError  string
}

type fleet struct {
	identity     *identityclient.Client
	vehicle      *vehicleclient.Client
	factoryToken *identityclient.CachedToken // factory workload token (bootstrap.provision)
	interval     time.Duration

	mu      sync.Mutex
	devices map[string]*device
}

func main() {
	idc := identityclient.New(env("IDENTITY_URL", "http://identity-service:8081"))
	factoryID := env("FACTORY_CLIENT_ID", "vehicle-factory")
	factorySecret := env("FACTORY_CLIENT_SECRET", "vehicle-factory-secret")

	f := &fleet{
		identity: idc,
		vehicle:  vehicleclient.New(env("VEHICLE_URL", "http://vehicle-service:8082")),
		interval: durationEnv("RECONCILE_INTERVAL", 8*time.Second),
		devices:  map[string]*device{},
	}
	f.factoryToken = identityclient.NewCachedToken(func(ctx context.Context) (identityclient.Token, error) {
		return idc.ServiceToken(ctx, factoryID, factorySecret, models.AudIdentityService, models.ScopeBootstrapProvision)
	})

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

// reconcile discovers all created vehicles, brings new ones online, and heartbeats
// the rest. Discovery uses the manufacturing persona: the simulator is the
// manufacturer's device fleet gateway.
func (f *fleet) reconcile(ctx context.Context) {
	dctx := httpx.WithCorrelationID(ctx, httpx.NewCorrelationID())
	vehicles, err := f.vehicle.List(dctx, models.PersonaManufacturing)
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
// The whole call-home flow (provision -> token -> register) shares one fresh
// correlation id so it can be traced end to end in the audit log.
func (f *fleet) bringOnline(ctx context.Context, d *device) {
	corr := httpx.NewCorrelationID()
	ctx = httpx.WithCorrelationID(ctx, corr)

	if d.secret == "" && !f.burnIn(ctx, d) {
		return
	}
	if d.token == nil {
		d.token = identityclient.NewCachedToken(func(c context.Context) (identityclient.Token, error) {
			return f.identity.BootstrapToken(c, d.vin, d.secret, models.AudVehicleService)
		})
	}
	bearer, err := d.token.Value(ctx)
	if err != nil {
		d.lastError = "bootstrap token: " + err.Error()
		return
	}
	v, err := f.vehicle.Register(ctx, bearer, corr, d.vin)
	if err != nil {
		d.lastError = "register: " + err.Error()
		log.Printf("[fleet] %s register not ready: %v", d.vin, err)
		return
	}
	d.vehicleID = v.ID
	d.registered = true
	d.lastError = ""
	log.Printf("[fleet] %s registered as %s", d.vin, d.vehicleID)
}

// burnIn provisions a fresh bootstrap credential for a VIN using a factory workload
// token. Returns true on success.
func (f *fleet) burnIn(ctx context.Context, d *device) bool {
	factoryBearer, err := f.factoryToken.Value(ctx)
	if err != nil {
		d.lastError = "factory token: " + err.Error()
		return false
	}
	secret := newSecret()
	if err := f.identity.ProvisionBootstrap(ctx, factoryBearer, d.vin, secret); err != nil {
		d.lastError = "provision: " + err.Error()
		log.Printf("[fleet] %s provision error: %v", d.vin, err)
		return false
	}
	d.secret = secret
	log.Printf("[fleet] %s burned in (factory provisioning)", d.vin)
	return true
}

func (f *fleet) heartbeat(ctx context.Context, d *device) {
	corr := httpx.NewCorrelationID()
	ctx = httpx.WithCorrelationID(ctx, corr)
	bearer, err := d.token.Value(ctx)
	if err != nil {
		d.lastError = "heartbeat token: " + err.Error()
		return
	}
	if err := f.vehicle.Heartbeat(ctx, bearer, corr, d.vehicleID); err != nil {
		d.lastError = "heartbeat: " + err.Error()
		return
	}
	d.lastBeat = time.Now()
	d.lastError = ""
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
