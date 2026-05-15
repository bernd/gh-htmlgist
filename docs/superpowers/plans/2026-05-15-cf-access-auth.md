# CF Access JWT Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Cloudflare Access JWT authentication middleware to htmlgist-proxy, adapted from the filer/auth.go pattern.

**Architecture:** New `internal/auth/` package with three files — `keystore.go` (JWKS fetching/caching), `validator.go` (JWT claim validation), `middleware.go` (HTTP handler wrapper). Wired in `cmd/htmlgist-proxy/main.go` to wrap the existing proxy handler. Existing packages unchanged.

**Tech Stack:** Go 1.26, `github.com/golang-jwt/jwt/v5`, `net/http`, `crypto/rsa`

---

### Task 1: Add jwt dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the dependency**

```bash
cd /Volumes/Projects/htmlgist && go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Tidy modules**

```bash
cd /Volumes/Projects/htmlgist && go mod tidy
```

- [ ] **Step 3: Verify**

```bash
cd /Volumes/Projects/htmlgist && grep golang-jwt go.mod
```

Expected: `github.com/golang-jwt/jwt/v5 v5.x.x`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: add golang-jwt/v5 dependency for CF Access auth"
```

---

### Task 2: Implement KeyStore with tests (TDD)

**Files:**
- Create: `internal/auth/keystore.go`
- Create: `internal/auth/keystore_test.go`

- [ ] **Step 1: Create test file with helpers and first test**

Create `internal/auth/keystore_test.go`:

```go
package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kroepke/gh-htmlgist/internal/auth"
)

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return key
}

func testJWKS(t *testing.T, keys map[string]*rsa.PublicKey) []byte {
	t.Helper()
	type jwk struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
		Alg string `json:"alg"`
		Use string `json:"use"`
	}
	type jwks struct {
		Keys []jwk `json:"keys"`
	}

	var ks jwks
	for kid, pub := range keys {
		ks.Keys = append(ks.Keys, jwk{
			Kty: "RSA",
			Kid: kid,
			N:   base64URLEncode(pub.N.Bytes()),
			E:   base64URLEncode(big.NewInt(int64(pub.E)).Bytes()),
			Alg: "RS256",
			Use: "sig",
		})
	}
	data, err := json.Marshal(ks)
	if err != nil {
		t.Fatalf("marshaling JWKS: %v", err)
	}
	return data
}

func TestKeyStore_FetchKeys(t *testing.T) {
	priv := generateTestKey(t)
	jwksData := testJWKS(t, map[string]*rsa.PublicKey{"key-1": &priv.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, err := ks.GetKey("key-1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if got.N.Cmp(priv.PublicKey.N) != 0 {
		t.Error("public key N mismatch")
	}
	if got.E != priv.PublicKey.E {
		t.Error("public key E mismatch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/...
```

Expected: compilation error — `auth` package doesn't exist yet.

- [ ] **Step 3: Implement KeyStore**

Create `internal/auth/keystore.go`:

```go
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const maxJWKSKeys = 20
const jwksFetchTimeout = 10 * time.Second

type KeyStore struct {
	teamURL            string
	mu                 sync.RWMutex
	keys               map[string]*rsa.PublicKey
	lastRefresh        time.Time
	minRefreshInterval time.Duration
}

func NewKeyStore(teamURL string) *KeyStore {
	return &KeyStore{
		teamURL:            teamURL,
		keys:               make(map[string]*rsa.PublicKey),
		minRefreshInterval: time.Minute,
	}
}

func (ks *KeyStore) GetKey(kid string) (*rsa.PublicKey, error) {
	ks.mu.RLock()
	key, ok := ks.keys[kid]
	ks.mu.RUnlock()
	if ok {
		return key, nil
	}

	if err := ks.refreshIfAllowed(); err != nil {
		return nil, fmt.Errorf("refreshing JWKS: %w", err)
	}

	ks.mu.RLock()
	key, ok = ks.keys[kid]
	ks.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
	return key, nil
}

func (ks *KeyStore) refreshIfAllowed() error {
	ks.mu.Lock()
	if time.Since(ks.lastRefresh) < ks.minRefreshInterval {
		ks.mu.Unlock()
		return nil
	}
	ks.lastRefresh = time.Now()
	ks.mu.Unlock()
	return ks.Refresh()
}

func (ks *KeyStore) Refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), jwksFetchTimeout)
	defer cancel()

	jwksURL := ks.teamURL + "/cdn-cgi/access/certs"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return fmt.Errorf("creating JWKS request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching JWKS from %s: %w", jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decoding JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, min(len(jwks.Keys), maxJWKSKeys))
	for i, k := range jwks.Keys {
		if i >= maxJWKSKeys {
			break
		}
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}

	if len(keys) == 0 {
		ks.mu.Lock()
		cachedCount := len(ks.keys)
		ks.lastRefresh = time.Now()
		ks.mu.Unlock()
		if cachedCount > 0 {
			return fmt.Errorf("JWKS returned zero usable keys; keeping %d cached keys", cachedCount)
		}
		return fmt.Errorf("JWKS returned zero usable keys")
	}

	ks.mu.Lock()
	ks.keys = keys
	ks.lastRefresh = time.Now()
	ks.mu.Unlock()

	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("decoding n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("decoding e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	if !e.IsInt64() || e.Int64() > math.MaxInt32 || e.Int64() < 1 {
		return nil, fmt.Errorf("RSA exponent out of range: %s", e.String())
	}

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

func (ks *KeyStore) StartBackgroundRefresh(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ks.Refresh(); err != nil {
				slog.Warn("JWKS background refresh failed", "error", err)
			}
		}
	}
}

// SetMinRefreshInterval overrides the rate limit for testing.
func (ks *KeyStore) SetMinRefreshInterval(d time.Duration) {
	ks.mu.Lock()
	ks.minRefreshInterval = d
	ks.mu.Unlock()
}
```

- [ ] **Step 4: Run first test**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestKeyStore_FetchKeys -v
```

Expected: PASS

- [ ] **Step 5: Add remaining KeyStore tests**

Append to `internal/auth/keystore_test.go`:

```go
func TestKeyStore_UnknownKid(t *testing.T) {
	priv := generateTestKey(t)
	jwksData := testJWKS(t, map[string]*rsa.PublicKey{"key-1": &priv.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	_, err := ks.GetKey("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
}

func TestKeyStore_KeyMissTriggersRefresh(t *testing.T) {
	priv1 := generateTestKey(t)
	priv2 := generateTestKey(t)

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			w.Write(testJWKS(t, map[string]*rsa.PublicKey{"key-1": &priv1.PublicKey}))
		} else {
			w.Write(testJWKS(t, map[string]*rsa.PublicKey{
				"key-1": &priv1.PublicKey,
				"key-2": &priv2.PublicKey,
			}))
		}
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	ks.SetMinRefreshInterval(0)

	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", callCount.Load())
	}

	// key-2 is missing; should trigger a re-fetch.
	got, err := ks.GetKey("key-2")
	if err != nil {
		t.Fatalf("GetKey key-2: %v", err)
	}
	if got.N.Cmp(priv2.PublicKey.N) != 0 {
		t.Error("key-2 public key N mismatch")
	}
	if callCount.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount.Load())
	}
}

func TestKeyStore_KeyMissRateLimit(t *testing.T) {
	priv := generateTestKey(t)
	jwksData := testJWKS(t, map[string]*rsa.PublicKey{"key-1": &priv.PublicKey})

	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	// Keep default 1-minute rate limit.

	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// First miss: rate-limited because Refresh() just set lastRefresh.
	_, err := ks.GetKey("unknown-1")
	if err == nil {
		t.Fatal("expected error for unknown kid")
	}
	// No additional fetch because we're within the rate limit window.
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call (rate limited), got %d", callCount.Load())
	}
}

func TestKeyStore_EmptyJWKSPreservesCache(t *testing.T) {
	key := generateTestKey(t)
	var returnEmpty atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if returnEmpty.Load() {
			w.Write([]byte(`{"keys":[]}`))
		} else {
			w.Write(testJWKS(t, map[string]*rsa.PublicKey{"key-1": &key.PublicKey}))
		}
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	returnEmpty.Store(true)
	err := ks.Refresh()
	if err == nil {
		t.Fatal("expected error for empty JWKS")
	}

	// Cached key should still work.
	got, err := ks.GetKey("key-1")
	if err != nil {
		t.Fatalf("GetKey after empty refresh: %v", err)
	}
	if got.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("cached key mismatch after empty refresh")
	}
}

func TestKeyStore_EndpointDownPreservesCache(t *testing.T) {
	key := generateTestKey(t)
	var unavailable atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(testJWKS(t, map[string]*rsa.PublicKey{"key-1": &key.PublicKey}))
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	unavailable.Store(true)
	if err := ks.Refresh(); err == nil {
		t.Fatal("expected error when endpoint is down")
	}

	got, err := ks.GetKey("key-1")
	if err != nil {
		t.Fatalf("GetKey after endpoint down: %v", err)
	}
	if got.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("cached key mismatch after endpoint down")
	}
}

func TestKeyStore_BackgroundRefresh(t *testing.T) {
	key := generateTestKey(t)
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(testJWKS(t, map[string]*rsa.PublicKey{"key-1": &key.PublicKey}))
	}))
	t.Cleanup(srv.Close)

	ks := auth.NewKeyStore(srv.URL)
	if err := ks.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	initial := callCount.Load()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ks.StartBackgroundRefresh(ctx, 10*time.Millisecond)
		close(done)
	}()

	// Wait for at least one background tick.
	deadline := time.After(time.Second)
	for callCount.Load() <= initial {
		select {
		case <-deadline:
			t.Fatal("background refresh did not fire within 1s")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	<-done
}
```

- [ ] **Step 6: Run all KeyStore tests**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestKeyStore -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/auth/keystore.go internal/auth/keystore_test.go
git commit -m "feat(auth): add KeyStore for JWKS fetching and caching"
```

---

### Task 3: Implement Validator with tests (TDD)

**Files:**
- Create: `internal/auth/validator.go`
- Create: `internal/auth/validator_test.go`

- [ ] **Step 1: Create test file with helpers and first test**

Create `internal/auth/validator_test.go`:

```go
package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestValidator_ValidToken -v
```

Expected: compilation error — `Validator` type doesn't exist yet.

- [ ] **Step 3: Implement Validator**

Create `internal/auth/validator.go`:

```go
package auth

import (
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/golang-jwt/jwt/v5"
	"time"
)

const clockSkewLeeway = 30 * time.Second

type Validator struct {
	keyStore *KeyStore
	issuer   string
	audience string
}

func NewValidator(keyStore *KeyStore, issuer, audience string) *Validator {
	return &Validator{
		keyStore: keyStore,
		issuer:   issuer,
		audience: audience,
	}
}

func (v *Validator) Validate(tokenString string) error {
	parser := jwt.NewParser(
		jwt.WithLeeway(clockSkewLeeway),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	)

	token, err := parser.Parse(tokenString, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid in token header")
		}
		return v.keyStore.GetKey(kid)
	})
	if err != nil {
		v.logAuthFailure(tokenString, err)
		return fmt.Errorf("invalid token: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("unexpected claims type")
	}
	if _, hasIat := claims["iat"]; !hasIat {
		return fmt.Errorf("missing required iat claim")
	}

	return nil
}

func (v *Validator) logAuthFailure(tokenStr string, err error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, _ := parser.ParseUnverified(tokenStr, jwt.MapClaims{})

	kid := "unknown"
	hashedSub := "unknown"

	if token != nil {
		if k, ok := token.Header["kid"].(string); ok {
			kid = k
		}
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if sub, ok := claims["sub"].(string); ok {
				hash := sha256.Sum256([]byte(sub))
				hashedSub = fmt.Sprintf("%x", hash[:4])
			}
		}
	}

	slog.Debug("auth validation failed",
		"reason", err.Error(),
		"kid", kid,
		"sub_hash", hashedSub,
	)
}
```

- [ ] **Step 4: Run first test**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestValidator_ValidToken -v
```

Expected: PASS

- [ ] **Step 5: Add remaining Validator tests**

Append to `internal/auth/validator_test.go`:

```go
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
```

- [ ] **Step 6: Run all Validator tests**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestValidator -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/auth/validator.go internal/auth/validator_test.go
git commit -m "feat(auth): add Validator for JWT claim validation"
```

---

### Task 4: Implement Middleware with tests (TDD)

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

- [ ] **Step 1: Create test file with first test**

Create `internal/auth/middleware_test.go`:

```go
package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestMiddleware_HealthBypassesAuth -v
```

Expected: compilation error — `Middleware` function doesn't exist.

- [ ] **Step 3: Implement Middleware**

Create `internal/auth/middleware.go`:

```go
package auth

import (
	"net/http"
)

func Middleware(v *Validator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/health/" {
			next.ServeHTTP(w, r)
			return
		}

		tokenStr := extractToken(r)
		if tokenStr == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if err := v.Validate(tokenStr); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Cf-Access-Jwt-Assertion"); h != "" {
		return h
	}
	if c, err := r.Cookie("CF_Authorization"); err == nil {
		return c.Value
	}
	return ""
}
```

- [ ] **Step 4: Run first test**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestMiddleware_HealthBypassesAuth -v
```

Expected: PASS

- [ ] **Step 5: Add remaining Middleware tests**

Append to `internal/auth/middleware_test.go`:

```go
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
```

- [ ] **Step 6: Run all Middleware tests**

```bash
cd /Volumes/Projects/htmlgist && go test ./internal/auth/... -run TestMiddleware -v
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat(auth): add HTTP middleware for CF Access token extraction"
```

---

### Task 5: Wire auth into proxy main

**Files:**
- Modify: `cmd/htmlgist-proxy/main.go`

- [ ] **Step 1: Update main.go**

Replace the contents of `cmd/htmlgist-proxy/main.go` with:

```go
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kroepke/gh-htmlgist/internal/auth"
	"github.com/kroepke/gh-htmlgist/internal/gist"
	"github.com/kroepke/gh-htmlgist/internal/proxy"
)

type tokenTransport struct {
	token   string
	wrapped http.RoundTripper
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.wrapped.RoundTrip(req)
}

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	cfTeamURL := os.Getenv("HTMLGIST_CF_TEAM_URL")
	if cfTeamURL == "" {
		log.Fatal("HTMLGIST_CF_TEAM_URL environment variable is required")
	}

	cfAudience := os.Getenv("HTMLGIST_CF_AUDIENCE")
	if cfAudience == "" {
		log.Fatal("HTMLGIST_CF_AUDIENCE environment variable is required")
	}

	addr := os.Getenv("HTMLGIST_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	keyStore := auth.NewKeyStore(cfTeamURL)
	if err := keyStore.Refresh(); err != nil {
		slog.Error("failed to fetch initial JWKS", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go keyStore.StartBackgroundRefresh(ctx, 5*time.Minute)

	validator := auth.NewValidator(keyStore, cfTeamURL, cfAudience)

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &tokenTransport{
			token:   token,
			wrapped: http.DefaultTransport,
		},
	}

	client := gist.NewClient(httpClient)
	handler := proxy.NewHandler(client)
	authed := auth.Middleware(validator, handler)

	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, authed))
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /Volumes/Projects/htmlgist && go build ./cmd/htmlgist-proxy/
```

Expected: builds successfully (binary created, or no error if using `go build`)

- [ ] **Step 3: Run all tests to verify no regressions**

```bash
cd /Volumes/Projects/htmlgist && go test ./...
```

Expected: all tests pass (auth tests + existing proxy/gist tests)

- [ ] **Step 4: Clean up build artifact**

```bash
rm -f /Volumes/Projects/htmlgist/htmlgist-proxy
```

- [ ] **Step 5: Run go fix**

```bash
cd /Volumes/Projects/htmlgist && go fix ./...
```

- [ ] **Step 6: Commit**

```bash
git add cmd/htmlgist-proxy/main.go
git commit -m "feat: wire CF Access auth middleware into proxy startup"
```

---

### Task 6: Update Dockerfile

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Read current Dockerfile**

Read `Dockerfile` to confirm current state before modifying.

- [ ] **Step 2: Verify Dockerfile still builds**

The Dockerfile shouldn't need changes — it already builds `cmd/htmlgist-proxy` which now imports `internal/auth`. But verify:

```bash
cd /Volumes/Projects/htmlgist && go build ./cmd/htmlgist-proxy/
```

Expected: builds without error. No Dockerfile changes needed unless the build breaks.

- [ ] **Step 3: Clean up**

```bash
rm -f /Volumes/Projects/htmlgist/htmlgist-proxy
```

- [ ] **Step 4: Final full test run**

```bash
cd /Volumes/Projects/htmlgist && go test ./...
```

Expected: all tests pass.
