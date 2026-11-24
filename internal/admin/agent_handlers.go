package admin

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
	"github.com/controlisai/recall-mcp-gateway/internal/store"
)

// configNotifier broadcasts agent mutations to other pods via PG LISTEN/NOTIFY.
type configNotifier interface {
	NotifyChange(configType string, data []byte) error
}

// agentIDRe validates agent IDs: alphanumeric + dot/dash/underscore/@, 1-255 chars.
var agentIDRe = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._@\-]{0,254}$`)

func isValidAgentID(id string) bool { return agentIDRe.MatchString(id) }

// AgentHandlers serves agent management endpoints.
type AgentHandlers struct {
	mu             sync.RWMutex
	agents         []config.AgentConfig
	persistPath    string   // path to agents.json (empty = no file persistence)
	persistDB      *sql.DB  // shared DB for agent persistence (nil = use file)
	lastSeen       map[string]time.Time // agentID -> last seen time (auto-discovered only)
	configNotifier configNotifier // PG LISTEN/NOTIFY for multi-pod (nil = standalone)
	// onUpdate is called when agent configs change (e.g., to refresh tool filter).
	onUpdate func(agents []config.AgentConfig)
}

// NewAgentHandlers creates agent API handlers.
func NewAgentHandlers(agents []config.AgentConfig, onUpdate func([]config.AgentConfig)) *AgentHandlers {
	copied := make([]config.AgentConfig, len(agents))
	copy(copied, agents)
	return &AgentHandlers{agents: copied, onUpdate: onUpdate, lastSeen: make(map[string]time.Time)}
}

// SetConfigNotifier wires PG LISTEN/NOTIFY for cross-pod agent change notification.
func (h *AgentHandlers) SetConfigNotifier(cn configNotifier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configNotifier = cn
}

// ReplaceAll atomically replaces all agents (used for PG LISTEN/NOTIFY reload).
func (h *AgentHandlers) ReplaceAll(agents []config.AgentConfig) {
	h.mu.Lock()
	h.agents = make([]config.AgentConfig, len(agents))
	copy(h.agents, agents)
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()
	if h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}
}

// SetPersistPath sets the file path for agent persistence and loads any persisted agents.
// Persisted agents are deduped against the current (YAML-configured) agent list.
func (h *AgentHandlers) SetPersistPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.persistPath = path

	persisted, err := loadAgentsFromFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("failed to load persisted agents", "component", "agents", "error", err)
		}
		return
	}

	// Build set of existing agent IDs (from YAML config)
	existing := make(map[string]bool, len(h.agents))
	for _, a := range h.agents {
		existing[a.ID] = true
	}

	// Merge persisted agents that don't conflict with YAML
	added := 0
	for _, pa := range persisted {
		if existing[pa.ID] {
			continue // YAML wins
		}
		h.agents = append(h.agents, pa)
		existing[pa.ID] = true
		added++
	}
	if added > 0 {
		slog.Info("loaded persisted agents", "component", "agents", "count", added, "path", path)
	}
}

// SetPersistDB configures agent handlers to persist agents in the shared gateway DB.
// In DB-only mode, agents are already loaded from DB before construction —
// this just sets the handle for future writes.
func (h *AgentHandlers) SetPersistDB(db *sql.DB) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.persistDB = db
}

// persist saves all agents to the DB and/or Redis.
func (h *AgentHandlers) persist() {
	if h.persistDB != nil {
		if err := saveAgentsToDB(h.persistDB, h.agents); err != nil {
			slog.Error("persist agents error", "component", "agents", "storage", "gateway.db", "error", err)
		}
	} else if h.persistPath != "" {
		if err := saveAgentsToFile(h.persistPath, h.agents); err != nil {
			slog.Error("persist error", "component", "agents", "error", err)
		}
	}
	if h.configNotifier != nil {
		data, err := json.Marshal(h.agents)
		if err != nil {
			slog.Error("failed to marshal agents for broadcast", "component", "agents", "error", err)
		} else if err := h.configNotifier.NotifyChange("agents", data); err != nil {
			slog.Warn("failed to notify agents change", "component", "agents", "error", err)
		}
	}
}

func saveAgentsToDB(db *sql.DB, agents []config.AgentConfig) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("DELETE FROM agents"); err != nil {
		return err
	}
	for _, a := range agents {
		data, err := json.Marshal(a)
		if err != nil {
			continue
		}
		allowedJSON, _ := json.Marshal(a.AllowedServers)
		blockedJSON, _ := json.Marshal(a.BlockedTools)
		hiddenJSON, _ := json.Marshal(a.HiddenTools)
		acknowledgedJSON, _ := json.Marshal(a.AcknowledgedTools)
		restricted := 0
		if a.ServerAccessRestricted {
			restricted = 1
		}
		_, err = store.ExecDialectTx(tx,
			`INSERT OR REPLACE INTO agents
			(id, token_hash, archetype, product, org_id, source, expires_at,
			 server_access_restricted, allowed_servers, blocked_tools, hidden_tools, acknowledged_tools, data)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO agents
			(id, token_hash, archetype, product, org_id, source, expires_at,
			 server_access_restricted, allowed_servers, blocked_tools, hidden_tools, acknowledged_tools, data)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO UPDATE SET
			 token_hash = $2, archetype = $3, product = $4, org_id = $5, source = $6,
			 expires_at = $7, server_access_restricted = $8, allowed_servers = $9,
			 blocked_tools = $10, hidden_tools = $11, acknowledged_tools = $12, data = $13`,
			store.IsPostgres(db),
			a.ID, a.TokenHash, a.Archetype, a.Product, a.OrgID, a.Source, a.ExpiresAt,
			restricted, string(allowedJSON), string(blockedJSON), string(hiddenJSON), string(acknowledgedJSON),
			string(data),
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func saveAgentsToFile(path string, agents []config.AgentConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadAgentsFromFile(path string) ([]config.AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var agents []config.AgentConfig
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// RegisterRoutes registers agent endpoints.
func (h *AgentHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/agents", h.handleList)
	mux.HandleFunc("/admin/v1/agents/", h.handleByID)
}

// AutoRegister adds an agent config if one doesn't already exist for this ID.
// Generates a Bearer token and persists. Returns true if a new agent was registered.
func (h *AgentHandlers) AutoRegister(agentID string) bool {
	if !isValidAgentID(agentID) {
		return false // reject invalid IDs silently
	}
	h.mu.Lock()
	for _, a := range h.agents {
		if a.ID == agentID {
			h.lastSeen[agentID] = time.Now()
			h.mu.Unlock()
			return false // already exists
		}
	}
	b := make([]byte, 32)
	rand.Read(b)
	token := "rp_" + hex.EncodeToString(b)
	h.agents = append(h.agents, config.AgentConfig{
		ID:        agentID,
		TokenHash: proxy.HashToken(token),
		Archetype: config.ArchetypeAutonomous,
		Source:    "auto-discovered",
	})
	h.lastSeen[agentID] = time.Now()
	h.persist()
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()

	if h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}
	return true
}

// PruneAutoDiscovered removes auto-discovered agents not seen within maxAge.
func (h *AgentHandlers) PruneAutoDiscovered(maxAge time.Duration) int {
	h.mu.Lock()
	cutoff := time.Now().Add(-maxAge)
	pruned := 0
	remaining := h.agents[:0]
	for _, a := range h.agents {
		if a.Source == "auto-discovered" {
			if seen, ok := h.lastSeen[a.ID]; ok && seen.Before(cutoff) {
				delete(h.lastSeen, a.ID)
				pruned++
				continue
			}
		}
		remaining = append(remaining, a)
	}
	h.agents = remaining
	if pruned > 0 {
		h.persist()
	}
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()

	if pruned > 0 && h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}
	return pruned
}

// Agents returns the current agent configs (thread-safe copy).
func (h *AgentHandlers) Agents() []config.AgentConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	copied := make([]config.AgentConfig, len(h.agents))
	copy(copied, h.agents)
	return copied
}

func (h *AgentHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		h.mu.RLock()
		defer h.mu.RUnlock()

		type agentResponse struct {
			ID                     string   `json:"id"`
			Archetype              string   `json:"archetype,omitempty"`
			Product                string   `json:"product,omitempty"`
			AllowedServers         []string `json:"allowedServers"`
			ServerAccessRestricted bool     `json:"serverAccessRestricted"`
			BlockedTools           []string `json:"blockedTools"`
			HiddenTools            []string `json:"hiddenTools"`
			AcknowledgedTools      []string `json:"acknowledgedTools"`
			OrgID                  string   `json:"orgId,omitempty"`
			HasToken               bool     `json:"hasToken"`
			Source                 string   `json:"source,omitempty"`
		}
		agents := make([]agentResponse, len(h.agents))
		for i, a := range h.agents {
			servers := a.AllowedServers
			if servers == nil {
				servers = []string{}
			}
			blocked := a.BlockedTools
			if blocked == nil {
				blocked = []string{}
			}
			hidden := a.HiddenTools
			if hidden == nil {
				hidden = []string{}
			}
			acked := a.AcknowledgedTools
			if acked == nil {
				acked = []string{}
			}
			agents[i] = agentResponse{
				ID:                     a.ID,
				Archetype:              a.Archetype,
				Product:                a.Product,
				AllowedServers:         servers,
				ServerAccessRestricted: a.ServerAccessRestricted,
				BlockedTools:           blocked,
				HiddenTools:            hidden,
				AcknowledgedTools:      acked,
				OrgID:                  a.OrgID,
				HasToken:               a.TokenHash != "",
				Source:                 a.Source,
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"agents": agents,
			"count":  len(agents),
		})

	case http.MethodPost:
		var body struct {
			ID             string   `json:"id"`
			Archetype      string   `json:"archetype,omitempty"`
			Product        string   `json:"product,omitempty"`
			AllowedServers []string `json:"allowedServers,omitempty"`
			GenerateToken  bool     `json:"generateToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
			return
		}
		if body.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "id is required"})
			return
		}
		if !isValidAgentID(body.ID) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid agent ID: must be 1-255 chars, alphanumeric/dash/underscore/dot/@"})
			return
		}

		h.mu.Lock()
		for _, a := range h.agents {
			if a.ID == body.ID {
				h.mu.Unlock()
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": "agent already exists"})
				return
			}
		}
		archetype := body.Archetype
		if archetype == "" {
			archetype = config.ArchetypeAutonomous
		}
		validArchetypes := map[string]bool{
			config.ArchetypeAutonomous: true,
			config.ArchetypeUserEmbedded: true,
			config.ArchetypeDelegated: true,
		}
		if !validArchetypes[archetype] {
			h.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid archetype: " + archetype})
			return
		}
		agent := config.AgentConfig{
			ID:             body.ID,
			Archetype:      archetype,
			Product:        body.Product,
			AllowedServers: body.AllowedServers,
			Source:         "dashboard",
		}
		var token string
		if body.GenerateToken {
			b := make([]byte, 32)
			rand.Read(b)
			token = "rp_" + hex.EncodeToString(b)
			agent.TokenHash = proxy.HashToken(token)
		}
		h.agents = append(h.agents, agent)
		h.persist()
		agentsCopy := make([]config.AgentConfig, len(h.agents))
		copy(agentsCopy, h.agents)
		h.mu.Unlock()

		if h.onUpdate != nil {
			h.onUpdate(agentsCopy)
		}

		resp := map[string]any{
			"status":  "created",
			"agentId": body.ID,
		}
		if token != "" {
			resp["token"] = token
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *AgentHandlers) handleByID(w http.ResponseWriter, r *http.Request) {
	// Parse: /admin/v1/agents/{id} or /admin/v1/agents/{id}/servers
	path := strings.TrimPrefix(r.URL.Path, "/admin/v1/agents/")
	parts := strings.SplitN(path, "/", 2)
	agentID := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	if agentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch subPath {
	case "servers":
		h.handleUpdateServers(w, r, agentID)
	case "tools":
		h.handleUpdateTools(w, r, agentID)
	case "rotate-token":
		h.handleRotateToken(w, r, agentID)
	case "reveal-token":
		h.handleRevealToken(w, r, agentID)
	case "":
		h.handleUpdateAgent(w, r, agentID)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *AgentHandlers) handleUpdateAgent(w http.ResponseWriter, r *http.Request, agentID string) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		h.mu.RLock()
		defer h.mu.RUnlock()
		for _, a := range h.agents {
			if a.ID == agentID {
				servers := a.AllowedServers
				if servers == nil {
					servers = []string{}
				}
				blocked := a.BlockedTools
				if blocked == nil {
					blocked = []string{}
				}
				hidden := a.HiddenTools
				if hidden == nil {
					hidden = []string{}
				}
				acked := a.AcknowledgedTools
				if acked == nil {
					acked = []string{}
				}
				json.NewEncoder(w).Encode(map[string]any{
					"id":                a.ID,
					"product":           a.Product,
					"allowedServers":    servers,
					"blockedTools":      blocked,
					"hiddenTools":       hidden,
					"acknowledgedTools": acked,
					"orgId":             a.OrgID,
					"hasToken":          a.TokenHash != "",
					"source":            a.Source,
				})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})

	case http.MethodDelete:
		h.mu.Lock()
		found := false
		for i := range h.agents {
			if h.agents[i].ID == agentID {
				h.agents = append(h.agents[:i], h.agents[i+1:]...)
				found = true
				break
			}
		}
		h.persist()
		agentsCopy := make([]config.AgentConfig, len(h.agents))
		copy(agentsCopy, h.agents)
		h.mu.Unlock()
		if !found {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})
			return
		}
		if h.onUpdate != nil {
			h.onUpdate(agentsCopy)
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "deleted", "agentId": agentID})

	case http.MethodPut:
		var body struct {
			AllowedServers    *[]string `json:"allowedServers,omitempty"`
			BlockedTools      *[]string `json:"blockedTools,omitempty"`
			HiddenTools       *[]string `json:"hiddenTools,omitempty"`
			AcknowledgedTools *[]string `json:"acknowledgedTools,omitempty"`
			OrgID             *string   `json:"orgId,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}

		// Validate blocked/hidden overlap if both are being set
		if body.BlockedTools != nil && body.HiddenTools != nil {
			if overlap := toolOverlap(*body.BlockedTools, *body.HiddenTools); overlap != "" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "tool " + overlap + " cannot be both blocked and hidden"})
				return
			}
		}

		h.mu.Lock()
		found := false
		for i := range h.agents {
			if h.agents[i].ID == agentID {
				// Cross-validate against existing values when only one list is updated
				if body.BlockedTools != nil && body.HiddenTools == nil {
					if overlap := toolOverlap(*body.BlockedTools, h.agents[i].HiddenTools); overlap != "" {
						h.mu.Unlock()
						w.WriteHeader(http.StatusBadRequest)
						json.NewEncoder(w).Encode(map[string]string{"error": "tool " + overlap + " cannot be both blocked and hidden"})
						return
					}
				}
				if body.HiddenTools != nil && body.BlockedTools == nil {
					if overlap := toolOverlap(h.agents[i].BlockedTools, *body.HiddenTools); overlap != "" {
						h.mu.Unlock()
						w.WriteHeader(http.StatusBadRequest)
						json.NewEncoder(w).Encode(map[string]string{"error": "tool " + overlap + " cannot be both blocked and hidden"})
						return
					}
				}
				if body.AllowedServers != nil {
					h.agents[i].AllowedServers = *body.AllowedServers
				}
				if body.BlockedTools != nil {
					h.agents[i].BlockedTools = *body.BlockedTools
				}
				if body.HiddenTools != nil {
					h.agents[i].HiddenTools = *body.HiddenTools
				}
				if body.AcknowledgedTools != nil {
					h.agents[i].AcknowledgedTools = *body.AcknowledgedTools
				}
				if body.OrgID != nil {
					h.agents[i].OrgID = *body.OrgID
				}
				found = true
				break
			}
		}
		agentsCopy := make([]config.AgentConfig, len(h.agents))
		copy(agentsCopy, h.agents)
		h.mu.Unlock()

		if !found {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})
			return
		}

		h.persist()

		if h.onUpdate != nil {
			h.onUpdate(agentsCopy)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"status":  "updated",
			"agentId": agentID,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *AgentHandlers) handleUpdateServers(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		AllowedServers []string `json:"allowedServers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	h.mu.Lock()
	found := false
	for i := range h.agents {
		if h.agents[i].ID == agentID {
			h.agents[i].AllowedServers = body.AllowedServers
			h.agents[i].ServerAccessRestricted = true // user explicitly configured access
			found = true
			break
		}
	}
	h.persist()
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()

	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})
		return
	}

	// Notify callback to refresh tool filter
	if h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":         "updated",
		"agentId":        agentID,
		"allowedServers": body.AllowedServers,
	})
}

func (h *AgentHandlers) handleUpdateTools(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		BlockedTools      []string `json:"blockedTools"`
		HiddenTools       []string `json:"hiddenTools"`
		AcknowledgedTools []string `json:"acknowledgedTools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	// A tool cannot be both blocked and hidden
	overlap := toolOverlap(body.BlockedTools, body.HiddenTools)
	if overlap != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "tool " + overlap + " cannot be both blocked and hidden"})
		return
	}

	h.mu.Lock()
	found := false
	for i := range h.agents {
		if h.agents[i].ID == agentID {
			h.agents[i].BlockedTools = body.BlockedTools
			h.agents[i].HiddenTools = body.HiddenTools
			h.agents[i].AcknowledgedTools = body.AcknowledgedTools
			found = true
			break
		}
	}
	h.persist()
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()

	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})
		return
	}

	if h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":       "updated",
		"agentId":      agentID,
		"blockedTools":      body.BlockedTools,
		"hiddenTools":       body.HiddenTools,
		"acknowledgedTools": body.AcknowledgedTools,
	})
}

// toolOverlap returns the first tool name that appears in both lists, or empty string.
func toolOverlap(a, b []string) string {
	set := make(map[string]bool, len(a))
	for _, t := range a {
		set[t] = true
	}
	for _, t := range b {
		if set[t] {
			return t
		}
	}
	return ""
}

func (h *AgentHandlers) handleRotateToken(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Optional request body for expires_at
	var body struct {
		ExpiresAt string `json:"expires_at"`
	}
	json.NewDecoder(r.Body).Decode(&body) // ignore error — body may be empty

	w.Header().Set("Content-Type", "application/json")

	if body.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, body.ExpiresAt); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "expires_at must be RFC3339 format (e.g. 2026-01-01T00:00:00Z)"})
			return
		}
	}

	h.mu.Lock()
	var newToken string
	found := false
	for i := range h.agents {
		if h.agents[i].ID == agentID {
			b := make([]byte, 32)
			rand.Read(b)
			newToken = "rp_" + hex.EncodeToString(b)
			h.agents[i].TokenHash = proxy.HashToken(newToken)
			if body.ExpiresAt != "" {
				h.agents[i].ExpiresAt = body.ExpiresAt
			}
			found = true
			break
		}
	}
	h.persist()
	agentsCopy := make([]config.AgentConfig, len(h.agents))
	copy(agentsCopy, h.agents)
	h.mu.Unlock()

	if !found {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "agent not found"})
		return
	}

	if h.onUpdate != nil {
		h.onUpdate(agentsCopy)
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status":  "rotated",
		"agentId": agentID,
		"token":   newToken,
	})
}

func (h *AgentHandlers) handleRevealToken(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Tokens are stored as SHA-256 hashes — cannot be revealed.
	// Use rotate-token to generate a new one (shown once).
	w.WriteHeader(http.StatusGone)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "tokens are stored as hashes and cannot be revealed. Use rotate-token to generate a new one.",
	})
}
