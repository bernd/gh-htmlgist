# htmlgist — Design Spec

A tool for sharing rendered HTML via GitHub Gists. Two components: a `gh` CLI extension for uploading, and an ultra-lightweight proxy for serving gists as live web pages.

## Problem

Sharing standalone HTML files (with CSS, images, etc.) internally is unnecessarily hard. GitHub Gists don't render HTML. Third-party gist renderers require trusting external infrastructure. We want something simple, self-hosted, and integrated into existing workflows.

## Architecture

Single Go module, two binaries:

```
htmlgist/
├── cmd/
│   ├── gh-htmlgist/          # CLI extension binary
│   └── htmlgist-proxy/       # Proxy server binary
├── internal/
│   ├── gist/                 # Shared GitHub Gist API client
│   └── proxy/                # HTTP handler logic
├── Dockerfile
├── .goreleaser.yml
├── go.mod
└── go.sum
```

Both binaries share the `internal/gist` package for GitHub API interaction.

## Component 1: CLI (`gh-htmlgist`)

A `gh` CLI extension. Installed via `gh extension install <org>/gh-htmlgist`. Inherits GitHub authentication from `gh auth`.

### Commands

- `gh htmlgist setup` — interactive prompt for proxy base URL, validates via health check, stores in `gh` config. Also triggered automatically on first `create` if not yet configured.
- `gh htmlgist create <file...>` — creates a secret gist from the given files. Prints the gist ID and proxy URL (if configured).
- `gh htmlgist update <gist-id> <file...>` — replaces files in an existing gist.
- `gh htmlgist list` — lists gists created via the tool, filtered by description convention (`[htmlgist]` prefix).
- `gh htmlgist delete <gist-id>` — deletes a gist.
- `gh htmlgist open <gist-id>` — opens the proxy URL in the default browser.

### Gist Visibility

All gists are created as **secret** (unlisted). Public gists are not supported. Secret gists are not discoverable but are accessible to anyone who knows the gist ID, which is sufficient since the proxy sits behind Cloudflare Access.

### Configuration

Stored via `gh config`:

- `extensions.htmlgist.proxy-url` — base URL of the proxy (e.g., `https://gist.internal.example.com`)

### Output

On `create` and `update`, the CLI prints:

```
Gist ID:    abc123def456...
GitHub URL: https://gist.github.com/abc123def456...
Proxy URL:  https://gist.internal.example.com/abc123def456...
```

The proxy URL line is only shown if `proxy-url` is configured.

## Component 2: Proxy (`htmlgist-proxy`)

A minimal HTTP server that fetches gist content from GitHub's API and serves it as rendered web pages.

### Routes

- `GET /health` — returns 200 OK. Used by setup validation and load balancers.
- `GET /<gist-id>/` — fetches the gist, finds the first `.html` file, serves it with `Content-Type: text/html`.
- `GET /<gist-id>/<filename>` — serves the named file from the gist with MIME type inferred from file extension.
- `GET /<gist-id>` — redirects to `/<gist-id>/` (trailing slash) so that relative paths in HTML resolve correctly.

### Relative Path Resolution

When a browser loads `/<gist-id>/` and the HTML contains `<link href="style.css">`, the browser resolves it to `/<gist-id>/style.css`. The proxy serves the matching file from the gist. No URL rewriting is needed.

### MIME Types

Inferred from file extension using Go's `mime.TypeByExtension`. Unknown extensions fall back to `application/octet-stream`.

### Error Handling

- Gist not found → `404` with plain-text message
- GitHub API error → `502` with generic message (no GitHub API details leaked)
- No `.html` file in gist → `404` with "no HTML file found in this gist"

### Caching

None. Cloudflare handles caching at the edge. The proxy fetches from GitHub on every request.

### Configuration

Environment variables only:

| Variable | Required | Default | Description |
|---|---|---|---|
| `HTMLGIST_ADDR` | No | `:8080` | Listen address |
| `GITHUB_TOKEN` | Yes | — | GitHub token for API access (any account, needs no special permissions) |

### Deployment Options

- Container (Docker/ECS/Fly.io) via included `Dockerfile`
- AWS Lambda behind API Gateway
- Bare binary on a spot instance
- Any platform that can run a static binary behind Cloudflare Access

## Component 3: Shared Package (`internal/gist`)

Wraps the GitHub Gist API. Used by both CLI and proxy.

### API

```go
type Client struct { ... }

func NewClient(httpClient *http.Client) *Client

func (c *Client) Create(files map[string][]byte, description string) (Gist, error) // auto-prefixes "[htmlgist] " to description
func (c *Client) Update(gistID string, files map[string][]byte) (Gist, error)
func (c *Client) Get(gistID string) (Gist, error)
func (c *Client) List() ([]Gist, error)
func (c *Client) Delete(gistID string) error
```

### Types

```go
type Gist struct {
    ID          string
    Description string
    Files       map[string]File
    HTMLURL     string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type File struct {
    Filename string
    Content  string
    Size     int
    Type     string
}
```

### Authentication

- **CLI context:** receives an authenticated `http.Client` from the `go-gh` library (inherits `gh auth`).
- **Proxy context:** receives an `http.Client` configured with the `GITHUB_TOKEN` env var.

### Description Convention

All gists created by the CLI use a `[htmlgist]` prefix in the description. This enables the `list` command to filter gists without a separate database.

## Security Model

- **Gist visibility:** Secret (unlisted). Not discoverable on GitHub, but accessible to anyone with the gist ID.
- **Proxy access control:** Cloudflare Access. The proxy itself has no authentication logic.
- **Gist ID as capability:** The 32-character hex gist ID acts as an unguessable token. Combined with Cloudflare Access, this provides two layers of protection.
- **Proxy GitHub token:** A fine-grained personal access token with `gist` read scope on any GitHub account. Only used for rate limit purposes (5,000 req/hr authenticated vs. 60/hr unauthenticated).

## Build & Distribution

### CI (GitHub Actions)

- **On push/PR:** `go vet`, `go test`, lint
- **On tag (`v*`):** GoReleaser builds both binaries for Linux/macOS/Windows (amd64 + arm64), creates a GitHub Release

### CLI Installation

```
gh extension install <org>/gh-htmlgist
```

The `gh` CLI downloads the correct binary for the user's platform from the GitHub Release assets.

### Proxy Deployment

- Release binaries downloadable from GitHub Releases
- `Dockerfile` in repo for container deployment (`FROM scratch` or `FROM alpine` with the static binary)
