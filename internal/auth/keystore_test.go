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
