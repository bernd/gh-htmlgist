package auth_test

import (
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kroepke/gh-htmlgist/internal/auth"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestMiddleware_HealthBypassesAuth(t *testing.T) {
	// Middleware with nil validator — would panic if auth ran on /health.
	handler := auth.Middleware(nil, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func setupMiddlewareValidator(t *testing.T) (*auth.Validator, *rsa.PrivateKey, string, string) {
	t.Helper()
	key := generateTestKey(t)
	kid := "key-1"
	audience := "test-aud"
	v, issuer := setupValidator(t, key, kid, audience)
	return v, key, issuer, kid
}

func TestMiddleware_MissingToken(t *testing.T) {
	v, _, _, _ := setupMiddlewareValidator(t)
	handler := auth.Middleware(v, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/some-gist/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	v, _, _, _ := setupMiddlewareValidator(t)
	handler := auth.Middleware(v, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/some-gist/", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", "not-a-valid-jwt")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_ValidTokenHeader(t *testing.T) {
	v, key, issuer, kid := setupMiddlewareValidator(t)
	handler := auth.Middleware(v, okHandler())

	token := createTestToken(t, key, kid, validClaims(issuer, "test-aud"))
	req := httptest.NewRequest(http.MethodGet, "/some-gist/", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", w.Body.String())
	}
}

func TestMiddleware_ValidTokenCookie(t *testing.T) {
	v, key, issuer, kid := setupMiddlewareValidator(t)
	handler := auth.Middleware(v, okHandler())

	token := createTestToken(t, key, kid, validClaims(issuer, "test-aud"))
	req := httptest.NewRequest(http.MethodGet, "/some-gist/", nil)
	req.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_HealthWithTrailingSlash(t *testing.T) {
	handler := auth.Middleware(nil, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/health/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
