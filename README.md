# htmlgist

Host HTML gists through a private, authenticated proxy. Write HTML files, publish them as GitHub Gists, and serve them through a Cloudflare Access-protected proxy.

Two components:

- **`gh-htmlgist`** -- a GitHub CLI extension for managing gists
- **`htmlgist-proxy`** -- an HTTP proxy that serves gist content behind Cloudflare Access

## Building

Requires Go 1.26+.

```sh
go build ./cmd/gh-htmlgist/
go build ./cmd/htmlgist-proxy/
```

Or use GoReleaser:

```sh
goreleaser build --snapshot --clean
```

### Docker (proxy only)

```sh
docker build -t htmlgist-proxy .
```

## Running the proxy

The proxy requires three environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | yes | GitHub personal access token with gist read access |
| `HTMLGIST_CF_TEAM_URL` | yes | Cloudflare Access team URL (e.g. `https://myteam.cloudflareaccess.com`) |
| `HTMLGIST_CF_AUDIENCE` | yes | Cloudflare Access application audience tag |
| `HTMLGIST_ADDR` | no | Listen address (default: `:8080`) |
| `HTMLGIST_AUTH_DISABLED` | no | Set to `true` to skip Cloudflare Access auth (development only) |

```sh
export GITHUB_TOKEN="ghp_..."
export HTMLGIST_CF_TEAM_URL="https://myteam.cloudflareaccess.com"
export HTMLGIST_CF_AUDIENCE="abc123..."
go run ./cmd/htmlgist-proxy/
```

The proxy fetches JWKS signing keys from Cloudflare on startup and refuses to start if the initial fetch fails. Keys are refreshed in the background every 5 minutes.

### Docker

```sh
docker run -p 8080:8080 \
  -e GITHUB_TOKEN="ghp_..." \
  -e HTMLGIST_CF_TEAM_URL="https://myteam.cloudflareaccess.com" \
  -e HTMLGIST_CF_AUDIENCE="abc123..." \
  htmlgist-proxy
```

### Routes

| Path | Auth | Description |
|------|------|-------------|
| `/health` | no | Health check, returns `ok` |
| `/{gistID}/` | yes | Serves `index.html` or the first HTML file in the gist |
| `/{gistID}/{filename}` | yes | Serves a specific file from the gist |

### Authentication

All routes except `/health` require a valid Cloudflare Access JWT. The proxy checks:

1. `Cf-Access-Jwt-Assertion` header (set by Cloudflare automatically)
2. `CF_Authorization` cookie (fallback)

The token must be RS256-signed by a key from the team's JWKS endpoint, and must have valid issuer, audience, expiration, and issued-at claims. A 30-second clock skew leeway is allowed.

### Local development

For development without Cloudflare Access configured, disable auth:

```sh
export GITHUB_TOKEN="ghp_..."
export HTMLGIST_AUTH_DISABLED=true
go run ./cmd/htmlgist-proxy/
```

For a production-like setup, use a [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) to run the proxy locally behind Cloudflare Access.

## Using the CLI

Install as a GitHub CLI extension:

```sh
gh extension install kroepke/gh-htmlgist
```

### Commands

```sh
gh htmlgist setup                  # Configure proxy URL
gh htmlgist create index.html      # Create a gist from files
gh htmlgist list                   # List your htmlgist gists
gh htmlgist update <id> index.html # Update a gist
gh htmlgist delete <id>            # Delete a gist
gh htmlgist open <id>              # Open in browser via proxy
```

The `create` command uploads files as a private GitHub Gist tagged with `[htmlgist]` and returns the proxy URL. The `open` command opens the gist through the configured proxy in your browser.

## Running tests

```sh
go test ./...
```
