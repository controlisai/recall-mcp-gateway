package admin

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// AdminAuthChecker provides shared admin endpoint authentication.
// When AdminKey is empty, all requests are allowed (dev/standalone mode).
// When AdminKey is set, requests must provide either:
//   - Authorization: Bearer <admin_key>
//   - A valid dashboard session cookie (via SessionValidator)
type AdminAuthChecker struct {
	AdminKey         string
	SessionValidator func(r *http.Request) bool
}

// CheckAuth verifies the request is authorized. Returns true if allowed.
// Writes 401/403 response and returns false if denied.
func (c *AdminAuthChecker) CheckAuth(w http.ResponseWriter, r *http.Request) bool {
	if c.AdminKey == "" {
		return true // no auth configured — open access (dev mode)
	}
	// Accept dashboard session cookie
	if c.SessionValidator != nil && c.SessionValidator(r) {
		return true
	}
	// Accept admin_key Bearer token
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(c.AdminKey)) != 1 {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return false
	}
	return true
}
