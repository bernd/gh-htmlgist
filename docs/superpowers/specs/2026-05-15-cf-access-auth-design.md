# Cloudflare Access JWT Authentication for htmlgist-proxy

## Overview

Add Cloudflare Access JWT validation to the htmlgist proxy as HTTP middleware. The auth layer validates tokens issued by Cloudflare Access before requests reach the proxy handler. Auth is always required — local development uses a CF tunnel.

The implementation adapts the proven pattern from `filer/auth.go` into htmlgist's own `internal/auth/` package.

## Requirements

- Validate Cloudflare Access JWTs on every request (except `/health`)
- Verify RS256 signature against JWKS public keys from CF
- Validate issuer, audience, expiration, and issued-at claims
- Proxy refuses to start without auth configuration
- No changes to existing proxy handler or gist packages

## Configuration

Two required environment variables:

| Variable | Purpose |
|----------|---------|
| `HTMLGIST_CF_TEAM_URL` | Cloudflare team URL. Used as JWKS endpoint (`{url}/cdn-cgi/access/certs`) and expected JWT issuer. |
| `HTMLGIST_CF_AUDIENCE` | Expected `aud` claim value, set in the CF Access application policy. |

Startup fails immediately if either is missing.

## Package Structure

```
internal/auth/
├── keystore.go        JWKS fetching, RSA key caching
├── validator.go       JWT parsing and claim validation
├── middleware.go       http.Handler wrapper
├── keystore_test.go
├── validator_test.go
└── middleware_test.go
```

Existing packages (`internal/proxy/`, `internal/gist/`) are unchanged.

## Components

### KeyStore (`keystore.go`)

Manages RSA public keys from Cloudflare's JWKS endpoint.

**Behavior:**
- Fetches JWKS from `{teamURL}/cdn-cgi/access/certs`
- Parses RSA public keys from JWK format (base64url-encoded `n` and `e`)
- Caches keys by `kid` (key ID)
- Background refresh goroutine runs every 5 minutes
- On-demand refresh when a requested `kid` is not in cache, rate-limited to once per minute
- If a JWKS fetch returns zero usable keys, the existing cache is preserved

**Types:**
```go
type KeyStore struct {
    teamURL            string
    mu                 sync.RWMutex
    keys               map[string]*rsa.PublicKey
    lastRefresh        time.Time
    minRefreshInterval time.Duration // default: 1 minute
}

func NewKeyStore(teamURL string) *KeyStore
func (ks *KeyStore) Refresh() error
func (ks *KeyStore) GetKey(kid string) (*rsa.PublicKey, error)
func (ks *KeyStore) StartBackgroundRefresh(ctx context.Context, interval time.Duration)
```

### Validator (`validator.go`)

Pure JWT validation — no HTTP coupling.

**Behavior:**
- Accepts a raw JWT token string
- Parses with RS256 algorithm restriction
- Validates: issuer (must match team URL), audience (must match configured value), expiration (required), issued-at (required, explicitly checked)
- 30-second clock skew leeway
- Looks up signing key by `kid` from KeyStore
- Privacy-preserving error logging: hashed subject (first 4 bytes of SHA256), kid

**Types:**
```go
type Validator struct {
    keyStore *KeyStore
    issuer   string
    audience string
}

func NewValidator(keyStore *KeyStore, issuer, audience string) *Validator
func (v *Validator) Validate(tokenString string) error
```

### Middleware (`middleware.go`)

HTTP middleware that connects token extraction to validation.

**Behavior:**
- Wraps an `http.Handler` and returns an `http.Handler`
- Exempts `/health` from auth (passes through directly)
- Extracts token from `Cf-Access-Jwt-Assertion` header, falls back to `CF_Authorization` cookie
- Calls `Validator.Validate(token)` — on success, calls next handler; on failure, returns 401

**Types:**
```go
func Middleware(v *Validator, next http.Handler) http.Handler
```

## Request Flow

```
HTTP Request
  │
  ▼
auth.Middleware
  ├── GET /health → bypass auth → proxy.Handler
  │
  ├── Extract token:
  │     1. Cf-Access-Jwt-Assertion header
  │     2. CF_Authorization cookie
  │     3. Neither → 401 Unauthorized
  │
  ├── validator.Validate(token)
  │     ├── Parse JWT (RS256 only)
  │     ├── Check iss, aud, exp, iat
  │     ├── Look up key by kid → KeyStore.GetKey()
  │     │     ├── Cache hit → return key
  │     │     └── Cache miss → rate-limited refresh → return key or error
  │     └── Verify RSA signature
  │
  ├── Valid → proxy.Handler.ServeHTTP(w, r)
  └── Invalid → 401 Unauthorized
```

## Wiring (`cmd/htmlgist-proxy/main.go`)

```go
cfTeamURL := os.Getenv("HTMLGIST_CF_TEAM_URL")
cfAudience := os.Getenv("HTMLGIST_CF_AUDIENCE")
// fail if either is empty

keyStore := auth.NewKeyStore(cfTeamURL)
if err := keyStore.Refresh(); err != nil {
    slog.Error("failed to fetch JWKS", "error", err)
    os.Exit(1)
}
go keyStore.StartBackgroundRefresh(ctx, 5*time.Minute)

validator := auth.NewValidator(keyStore, cfTeamURL, cfAudience)
handler := proxy.NewHandler(gistClient)
authed := auth.Middleware(validator, handler)

http.ListenAndServe(addr, authed)
```

## Dependencies

New: `github.com/golang-jwt/jwt/v5`

## Testing

### keystore_test.go
- JWKS fetching and RSA key parsing from mock HTTP server
- Key caching: second fetch returns cached key without HTTP call
- Cache replacement on refresh
- Rate-limited refresh on key miss (second miss within 1 minute does not re-fetch)
- Cache preservation when JWKS endpoint returns empty keys
- Cache preservation when JWKS endpoint is unreachable

### validator_test.go
Tests mint RSA key pairs and sign JWTs in-test.
- Valid token passes validation
- Wrong audience rejected
- Wrong issuer rejected
- Expired token rejected
- Missing `exp` claim rejected
- Missing `iat` claim rejected
- Clock skew within 30s leeway accepted
- Wrong algorithm (e.g. HS256) rejected
- Missing `kid` in token header rejected
- Unknown `kid` rejected (after refresh attempt)

### middleware_test.go
- `/health` bypasses auth, returns 200
- Missing token returns 401
- Invalid token returns 401
- Valid token passes through to next handler

No integration tests with real Cloudflare — unit tests with self-signed RSA keys cover the validation logic.

## Security Considerations

| Threat | Mitigation |
|--------|-----------|
| Token tampering | RSA signature verification with JWKS public keys |
| Replay attacks | Mandatory `exp` + `iat` claim validation |
| Key rotation | Background refresh (5 min) + on-demand refresh on kid miss |
| Wrong authority | Issuer must match configured team URL |
| Cross-service auth | Audience must match configured application value |
| Clock skew exploits | 30-second leeway with required expiration |
| JWKS outage | Cached keys continue to work |
