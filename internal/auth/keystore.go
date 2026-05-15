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
