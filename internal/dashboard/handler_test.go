package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
	"golang.org/x/crypto/bcrypt"
)

// testPassHash generates a bcrypt hash for test use.
func testPassHash(password string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(h)
}

func TestDashboard_ServesHTML(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "", "", context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/dashboard/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if len(body) < 100 {
		t.Error("expected HTML content")
	}
	if w.Header().Get("Content-Type") == "" {
		t.Error("expected Content-Type header")
	}
}

func TestDashboard_Redirect(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "", "", context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", w.Code)
	}
}

func TestDashboard_SSE_Endpoint(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "", "", context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/ws/audit", nil)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(w, req)
		close(done)
	}()

	req.Body.Close()
}

func TestDashboard_BasicAuth_Rejects(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "admin", testPassHash("secret123"), context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/dashboard/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", w.Code)
	}
}

func TestDashboard_SessionAuth_LoginAndAccess(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "admin", testPassHash("secret123"), context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Without session → 401
	req := httptest.NewRequest("GET", "/dashboard/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", w.Code)
	}

	// Login with correct password → 303 + session cookie
	form := "username=admin&password=secret123"
	req = httptest.NewRequest("POST", "/dashboard/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: status = %d, want 303", w.Code)
	}
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "recall_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	// Access dashboard with session → 200
	req = httptest.NewRequest("GET", "/dashboard/", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("with session: status = %d, want 200", w.Code)
	}
}

func TestDashboard_SessionAuth_WrongPassword(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "admin", testPassHash("secret123"), context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	form := "username=admin&password=wrong"
	req := httptest.NewRequest("POST", "/dashboard/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong pass: status = %d, want 401", w.Code)
	}
}

func TestDashboard_SessionAuth_Logout(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)

	handler, err := NewHandler(pool, auditor, nil, nil, "dev", "admin", testPassHash("secret123"), context.Background())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Login
	form := "username=admin&password=secret123"
	req := httptest.NewRequest("POST", "/dashboard/login", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: status = %d, want 303", w.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "recall_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	// Logout
	req = httptest.NewRequest("GET", "/dashboard/logout", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("logout: status = %d, want 303", w.Code)
	}

	// Old session cookie should no longer work
	req = httptest.NewRequest("GET", "/dashboard/", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("after logout: status = %d, want 401", w.Code)
	}
}
