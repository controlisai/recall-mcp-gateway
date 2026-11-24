package admin

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/auth"
)

// RevocationHandler provides the POST /admin/v1/revoke endpoint.
type RevocationHandler struct {
	store    *auth.RevocationStore
	adminKey string
}

// NewRevocationHandler creates a revocation endpoint handler.
func NewRevocationHandler(store *auth.RevocationStore, adminKey string) *RevocationHandler {
	return &RevocationHandler{store: store, adminKey: adminKey}
}

// RegisterRoutes mounts the revocation endpoint.
func (h *RevocationHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/revoke", h.handleRevoke)
}

func (h *RevocationHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Auth: require admin key
	if h.adminKey != "" {
		authHeader := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(authHeader), []byte("Bearer "+h.adminKey)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	var body struct {
		Token     string `json:"token"`      // raw token to revoke
		TokenHash string `json:"token_hash"` // or pre-computed hash
		ExpiresAt string `json:"expires_at"` // RFC3339, defaults to 1 hour
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if body.Token == "" && body.TokenHash == "" {
		http.Error(w, `{"error":"token or token_hash required"}`, http.StatusBadRequest)
		return
	}

	expiresAt := time.Now().Add(1 * time.Hour) // default: assume 1h token lifetime
	if body.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, body.ExpiresAt); err == nil {
			expiresAt = t
		}
	}

	if body.Token != "" {
		h.store.RevokeRaw(body.Token, expiresAt)
		slog.Info("token revoked", "component", "revocation", "method", "raw")
	} else {
		h.store.Revoke(body.TokenHash, expiresAt)
		slog.Info("token revoked", "component", "revocation", "method", "hash")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":     "revoked",
		"revoked_at": time.Now().Format(time.RFC3339),
		"count":      h.store.Count(),
	})
}
