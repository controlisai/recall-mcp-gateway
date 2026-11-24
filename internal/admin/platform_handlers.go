package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/platform"
)

// ConnectParams holds the parameters for a platform connection.
type ConnectParams struct {
	ConsoleAPIURL    string `json:"consoleApiUrl"`
	ServiceToken     string `json:"serviceToken"`
	TenantID         string `json:"tenantId"`
	EngineURL        string `json:"engineUrl"`
	APIKey           string `json:"apiKey"`
	DefaultOrgID     string `json:"defaultOrgId"`
	ConsoleTenantURL string `json:"consoleTenantUrl"` // optional — UI link target
}

// PlatformHandlers proxies platform data to the embedded dashboard.
type PlatformHandlers struct {
	mu         sync.RWMutex
	client     *platform.Client
	productID  string
	consoleURL string
	onConnect  func(params ConnectParams) // callback to wire engine bridge + platform client
}

// NewPlatformHandlers creates platform handlers.
// If client is nil (standalone mode), endpoints return {connected: false}.
func NewPlatformHandlers(client *platform.Client, productID, consoleURL string) *PlatformHandlers {
	return &PlatformHandlers{
		client:     client,
		productID:  productID,
		consoleURL: consoleURL,
	}
}

// SetOnConnect sets the callback invoked when platform connection is established.
func (h *PlatformHandlers) SetOnConnect(fn func(params ConnectParams)) {
	h.onConnect = fn
}

// Client returns the current platform client (for use by sync handlers).
func (h *PlatformHandlers) Client() *platform.Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.client
}

// SetProductID sets the selected product ID (after sync).
func (h *PlatformHandlers) SetProductID(pid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.productID = pid
}

// RegisterRoutes mounts platform routes on the mux.
func (h *PlatformHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/platform/summary", h.handleSummary)
	mux.HandleFunc("/admin/v1/platform/agents", h.handleAgents)
	mux.HandleFunc("/admin/v1/platform/drift", h.handleDrift)
	mux.HandleFunc("/admin/v1/platform/orgs", h.handleOrgs)
	mux.HandleFunc("/admin/v1/platform/connect", h.handleConnect)
	mux.HandleFunc("/admin/v1/platform/products", h.handleProducts)
}

func (h *PlatformHandlers) handleSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.client == nil || !h.client.IsConfigured() {
		json.NewEncoder(w).Encode(platform.PlatformSummary{Connected: false})
		return
	}

	summary := h.client.FetchSummary(r.Context(), h.productID, h.consoleURL)
	json.NewEncoder(w).Encode(summary)
}

func (h *PlatformHandlers) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.client == nil || !h.client.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]any{"connected": false, "agents": []any{}})
		return
	}

	agents, err := h.client.FetchAgentHealth(r.Context(), h.productID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "agents": []any{}})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"agents": agents})
}

func (h *PlatformHandlers) handleDrift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.client == nil || !h.client.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]any{"connected": false, "events": []any{}})
		return
	}

	events, _ := h.client.FetchDriftEvents(r.Context(), h.productID)
	json.NewEncoder(w).Encode(map[string]any{"events": events})
}

func (h *PlatformHandlers) handleOrgs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.client == nil || !h.client.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]any{"connected": false, "orgs": []any{}})
		return
	}

	orgs, _ := h.client.FetchOrgSummary(r.Context(), h.productID)
	json.NewEncoder(w).Encode(map[string]any{"orgs": orgs})
}

func (h *PlatformHandlers) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var params ConnectParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if params.ConsoleAPIURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "consoleApiUrl is required"})
		return
	}

	if u, err := url.Parse(params.ConsoleAPIURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "consoleApiUrl must be http or https"})
		return
	}
	if params.EngineURL != "" {
		if u, err := url.Parse(params.EngineURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "engineUrl must be http or https"})
			return
		}
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}

	// 1. Validate console-api (try /health and /healthz)
	if !checkHealth(httpClient, params.ConsoleAPIURL, params.ServiceToken, "Authorization", "Bearer ") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Cannot reach Console API at %s", params.ConsoleAPIURL)})
		return
	}

	// 2. Validate engine (optional — warn if unreachable but don't block)
	engineReachable := false
	if params.EngineURL != "" {
		if params.APIKey == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "apiKey is required when engineUrl is provided"})
			return
		}
		engineReachable = checkHealth(httpClient, params.EngineURL, params.APIKey, "Authorization", "Bearer ")
	}

	// 3. Update platform client
	h.mu.Lock()
	if h.client == nil {
		h.client = platform.NewClient(params.ConsoleAPIURL, params.ServiceToken)
	} else {
		h.client.UpdateConnection(params.ConsoleAPIURL, params.ServiceToken, params.TenantID)
	}
	h.client.SetTenantID(params.TenantID)
	if params.ConsoleTenantURL != "" {
		h.consoleURL = params.ConsoleTenantURL
	} else {
		h.consoleURL = params.ConsoleAPIURL // fallback
	}
	h.mu.Unlock()

	// 4. Notify callback (wires engine bridge + platform client)
	if h.onConnect != nil {
		h.onConnect(params)
	}

	// 5. Build response
	h.mu.RLock()
	consoleURL := h.consoleURL
	h.mu.RUnlock()
	result := map[string]any{
		"status":     "connected",
		"consoleUrl": consoleURL,
	}
	if params.EngineURL != "" {
		result["engineConnected"] = engineReachable
		if !engineReachable {
			result["engineWarning"] = "Engine not reachable — two-layer evaluation disabled. Check engine URL and API key."
		}
	}

	// Fetch products for the dropdown
	products, pErr := h.client.FetchProducts(r.Context())
	if pErr != nil {
		result["productError"] = pErr.Error()
	}
	if len(products) > 0 {
		result["products"] = products
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *PlatformHandlers) handleProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if h.client == nil || !h.client.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]any{"connected": false, "products": []any{}})
		return
	}

	products, err := h.client.FetchProducts(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "products": []any{}})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"products": products})
}

// checkHealth tries /health and /healthz on a service to see if it's reachable.
func checkHealth(client *http.Client, baseURL, token, headerName, headerPrefix string) bool {
	for _, path := range []string{"/health", "/healthz"} {
		req, err := http.NewRequest("GET", baseURL+path, nil)
		if err != nil {
			continue
		}
		if token != "" {
			req.Header.Set(headerName, headerPrefix+token)
		}
		resp, err := client.Do(req)
		if err != nil || resp == nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return true
		}
	}
	return false
}
