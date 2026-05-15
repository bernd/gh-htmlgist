package auth_test

import (
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kroepke/gh-htmlgist/internal/auth"
)

func createTestToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signed
}

func setupValidator(t *testing.T, key *rsa.PrivateKey, kid, audience string) (*auth.Validator, string) {
	t.Helper()
	jwksData := testJWKS(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	v := auth.NewValidator(ks, srv.URL, audience)
	return v, srv.URL
}

func validClaims(issuer, audience string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"sub": "user-123",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}
}

func TestValidator_ValidToken(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	token := createTestToken(t, key, "key-1", validClaims(issuer, "test-aud"))

	if err := v.Validate(token); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidator_WrongAudience(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	token := createTestToken(t, key, "key-1", validClaims(issuer, "wrong-aud"))

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidator_WrongIssuer(t *testing.T) {
	key := generateTestKey(t)
	v, _ := setupValidator(t, key, "key-1", "test-aud")
	token := createTestToken(t, key, "key-1", validClaims("https://evil.example.com", "test-aud"))

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidator_ExpiredToken(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")
	claims["exp"] = time.Now().Add(-5 * time.Minute).Unix()
	claims["iat"] = time.Now().Add(-10 * time.Minute).Unix()
	token := createTestToken(t, key, "key-1", claims)

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidator_MissingExp(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")
	delete(claims, "exp")
	token := createTestToken(t, key, "key-1", claims)

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for missing exp")
	}
}

func TestValidator_MissingIat(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")
	delete(claims, "iat")
	token := createTestToken(t, key, "key-1", claims)

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for missing iat")
	}
}

func TestValidator_ClockSkewWithinLeeway(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")
	claims["exp"] = time.Now().Add(-20 * time.Second).Unix()
	claims["iat"] = time.Now().Add(-10 * time.Minute).Unix()
	token := createTestToken(t, key, "key-1", claims)

	if err := v.Validate(token); err != nil {
		t.Fatalf("expected valid within leeway, got: %v", err)
	}
}

func TestValidator_MissingKid(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	delete(tok.Header, "kid")
	tokenStr, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	err = v.Validate(tokenStr)
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestValidator_UnknownKid(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	token := createTestToken(t, key, "unknown-kid", validClaims(issuer, "test-aud"))

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestValidator_WrongSigningKey(t *testing.T) {
	legitimateKey := generateTestKey(t)
	attackerKey := generateTestKey(t)
	v, issuer := setupValidator(t, legitimateKey, "key-1", "test-aud")
	token := createTestToken(t, attackerKey, "key-1", validClaims(issuer, "test-aud"))

	err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestValidator_AudAsArray(t *testing.T) {
	key := generateTestKey(t)
	v, issuer := setupValidator(t, key, "key-1", "test-aud")
	claims := validClaims(issuer, "test-aud")
	claims["aud"] = []string{"other-aud", "test-aud"}
	token := createTestToken(t, key, "key-1", claims)

	if err := v.Validate(token); err != nil {
		t.Fatalf("expected valid with aud array, got: %v", err)
	}
}
