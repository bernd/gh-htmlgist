# htmlgist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `gh` CLI extension for uploading HTML files as secret GitHub Gists, and an ultra-lightweight proxy server for rendering them as live web pages.

**Architecture:** Single Go module with two binaries (`cmd/gh-htmlgist` and `cmd/htmlgist-proxy`), sharing a `internal/gist` package for GitHub API interaction and a `internal/proxy` package for the HTTP handler. The CLI inherits auth from `gh`, the proxy reads `GITHUB_TOKEN` from the environment.

**Tech Stack:** Go, `github.com/cli/go-gh/v2` (for CLI auth/browser), GitHub Gist API, `net/http` (proxy), GoReleaser (builds), GitHub Actions (CI)

---

## File Structure

```
go.mod
internal/gist/gist.go            # Domain types: Gist, File, NotFoundError, API JSON types
internal/gist/client.go           # Client: Get, Create, Update, Delete, List
internal/gist/client_test.go      # Tests against httptest mock server
internal/proxy/handler.go         # GistFetcher interface, Handler (ServeHTTP)
internal/proxy/handler_test.go    # Tests with mock GistFetcher
cmd/htmlgist-proxy/main.go        # Proxy entry: env config, token transport, ListenAndServe
cmd/gh-htmlgist/main.go           # CLI entry: subcommand routing, all commands
Dockerfile                        # Multi-stage build for proxy binary
.goreleaser.yml                   # Cross-compile both binaries
.github/workflows/ci.yml          # go vet, test, lint on push/PR
.github/workflows/release.yml     # GoReleaser on v* tags
```

---

### Task 1: Project Scaffolding & Types

**Files:**
- Create: `go.mod`
- Create: `internal/gist/gist.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/kroepke/Projects/htmlgist
go mod init github.com/kroepke/gh-htmlgist
```

Replace `kroepke` with your GitHub org/username if different.

- [ ] **Step 2: Create domain types**

Create `internal/gist/gist.go`:

```go
package gist

import (
	"fmt"
	"time"
)

const DescriptionPrefix = "[htmlgist] "

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

type NotFoundError struct {
	GistID string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("gist not found: %s", e.GistID)
}

type apiGist struct {
	ID          string             `json:"id"`
	Description string             `json:"description"`
	HTMLURL     string             `json:"html_url"`
	Public      bool               `json:"public"`
	Files       map[string]apiFile `json:"files"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type apiFile struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Size     int    `json:"size"`
	Content  string `json:"content"`
}

func (ag *apiGist) toGist() Gist {
	files := make(map[string]File, len(ag.Files))
	for name, f := range ag.Files {
		files[name] = File{
			Filename: f.Filename,
			Content:  f.Content,
			Size:     f.Size,
			Type:     f.Type,
		}
	}
	return Gist{
		ID:          ag.ID,
		Description: ag.Description,
		Files:       files,
		HTMLURL:     ag.HTMLURL,
		CreatedAt:   ag.CreatedAt,
		UpdatedAt:   ag.UpdatedAt,
	}
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod internal/gist/gist.go
git commit -m "feat: initialize module and define gist domain types"
```

---

### Task 2: Gist Client — Get

**Files:**
- Create: `internal/gist/client.go`
- Create: `internal/gist/client_test.go`

- [ ] **Step 1: Write failing tests for Get**

Create `internal/gist/client_test.go`:

```go
package gist_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kroepke/gh-htmlgist/internal/gist"
)

func TestClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/gists/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "abc123",
			"description": "[htmlgist] test page",
			"html_url":    "https://gist.github.com/abc123",
			"public":      false,
			"files": map[string]interface{}{
				"index.html": map[string]interface{}{
					"filename": "index.html",
					"type":     "text/html",
					"size":     14,
					"content":  "<h1>hello</h1>",
				},
			},
			"created_at": "2026-05-15T00:00:00Z",
			"updated_at": "2026-05-15T00:00:00Z",
		})
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	g, err := c.Get("abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.ID != "abc123" {
		t.Errorf("expected ID abc123, got %s", g.ID)
	}
	if g.Description != "[htmlgist] test page" {
		t.Errorf("unexpected description: %s", g.Description)
	}
	f, ok := g.Files["index.html"]
	if !ok {
		t.Fatal("expected index.html in files")
	}
	if f.Content != "<h1>hello</h1>" {
		t.Errorf("unexpected content: %s", f.Content)
	}
	if f.Size != 14 {
		t.Errorf("expected size 14, got %d", f.Size)
	}
}

func TestClientGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	_, err := c.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	nfe, ok := err.(*gist.NotFoundError)
	if !ok {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	if nfe.GistID != "nonexistent" {
		t.Errorf("expected gist ID nonexistent, got %s", nfe.GistID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/gist/ -v
```

Expected: compilation error — `gist.NewClient` not defined.

- [ ] **Step 3: Implement Client and Get**

Create `internal/gist/client.go`:

```go
package gist

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{
		HTTPClient: httpClient,
		BaseURL:    "https://api.github.com",
	}
}

func (c *Client) Get(gistID string) (Gist, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/gists/" + gistID)
	if err != nil {
		return Gist{}, fmt.Errorf("fetching gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Gist{}, &NotFoundError{GistID: gistID}
	}
	if resp.StatusCode != http.StatusOK {
		return Gist{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var ag apiGist
	if err := json.NewDecoder(resp.Body).Decode(&ag); err != nil {
		return Gist{}, fmt.Errorf("decoding response: %w", err)
	}

	return ag.toGist(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gist/ -v
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gist/client.go internal/gist/client_test.go
git commit -m "feat: add gist client with Get method"
```

---

### Task 3: Gist Client — Create

**Files:**
- Modify: `internal/gist/client.go`
- Modify: `internal/gist/client_test.go`

- [ ] **Step 1: Write failing test for Create**

Add to `internal/gist/client_test.go`:

```go
func TestClientCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/gists" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["public"] != false {
			t.Errorf("expected public=false, got %v", body["public"])
		}

		desc, _ := body["description"].(string)
		if desc != "[htmlgist] my page" {
			t.Errorf("unexpected description: %s", desc)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "new123",
			"description": "[htmlgist] my page",
			"html_url":    "https://gist.github.com/new123",
			"public":      false,
			"files": map[string]interface{}{
				"index.html": map[string]interface{}{
					"filename": "index.html",
					"type":     "text/html",
					"size":     14,
					"content":  "<h1>hello</h1>",
				},
			},
			"created_at": "2026-05-15T00:00:00Z",
			"updated_at": "2026-05-15T00:00:00Z",
		})
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	files := map[string][]byte{
		"index.html": []byte("<h1>hello</h1>"),
	}

	g, err := c.Create(files, "my page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.ID != "new123" {
		t.Errorf("expected ID new123, got %s", g.ID)
	}
	if g.Description != "[htmlgist] my page" {
		t.Errorf("unexpected description: %s", g.Description)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/gist/ -run TestClientCreate -v
```

Expected: compilation error — `c.Create` not defined.

- [ ] **Step 3: Implement Create**

Add to `internal/gist/client.go` (add `"bytes"` to the import block):

```go
func (c *Client) Create(files map[string][]byte, description string) (Gist, error) {
	apiFiles := make(map[string]interface{}, len(files))
	for name, content := range files {
		apiFiles[name] = map[string]string{
			"content": string(content),
		}
	}

	body := map[string]interface{}{
		"description": DescriptionPrefix + description,
		"public":      false,
		"files":       apiFiles,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return Gist{}, fmt.Errorf("encoding request: %w", err)
	}

	resp, err := c.HTTPClient.Post(c.BaseURL+"/gists", "application/json", &buf)
	if err != nil {
		return Gist{}, fmt.Errorf("creating gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return Gist{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var ag apiGist
	if err := json.NewDecoder(resp.Body).Decode(&ag); err != nil {
		return Gist{}, fmt.Errorf("decoding response: %w", err)
	}

	return ag.toGist(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/gist/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gist/client.go internal/gist/client_test.go
git commit -m "feat: add gist Create method with secret visibility"
```

---

### Task 4: Gist Client — List, Update, Delete

**Files:**
- Modify: `internal/gist/client.go`
- Modify: `internal/gist/client_test.go`

- [ ] **Step 1: Write failing tests for List, Update, Delete**

Add to `internal/gist/client_test.go`:

```go
func TestClientList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/gists" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("expected per_page=100, got %s", r.URL.Query().Get("per_page"))
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"id":          "gist1",
				"description": "[htmlgist] page one",
				"html_url":    "https://gist.github.com/gist1",
				"public":      false,
				"files":       map[string]interface{}{},
				"created_at":  "2026-05-15T00:00:00Z",
				"updated_at":  "2026-05-15T00:00:00Z",
			},
			{
				"id":          "gist2",
				"description": "some other gist",
				"html_url":    "https://gist.github.com/gist2",
				"public":      true,
				"files":       map[string]interface{}{},
				"created_at":  "2026-05-15T00:00:00Z",
				"updated_at":  "2026-05-15T00:00:00Z",
			},
			{
				"id":          "gist3",
				"description": "[htmlgist] page two",
				"html_url":    "https://gist.github.com/gist3",
				"public":      false,
				"files":       map[string]interface{}{},
				"created_at":  "2026-05-15T00:00:00Z",
				"updated_at":  "2026-05-15T00:00:00Z",
			},
		})
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	gists, err := c.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gists) != 2 {
		t.Fatalf("expected 2 gists, got %d", len(gists))
	}
	if gists[0].ID != "gist1" {
		t.Errorf("expected first gist ID gist1, got %s", gists[0].ID)
	}
	if gists[1].ID != "gist3" {
		t.Errorf("expected second gist ID gist3, got %s", gists[1].ID)
	}
}

func TestClientUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/gists/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          "abc123",
			"description": "[htmlgist] updated",
			"html_url":    "https://gist.github.com/abc123",
			"public":      false,
			"files": map[string]interface{}{
				"index.html": map[string]interface{}{
					"filename": "index.html",
					"type":     "text/html",
					"size":     16,
					"content":  "<h1>updated</h1>",
				},
			},
			"created_at": "2026-05-15T00:00:00Z",
			"updated_at": "2026-05-15T01:00:00Z",
		})
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	files := map[string][]byte{
		"index.html": []byte("<h1>updated</h1>"),
	}

	g, err := c.Update("abc123", files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.ID != "abc123" {
		t.Errorf("expected ID abc123, got %s", g.ID)
	}
	f := g.Files["index.html"]
	if f.Content != "<h1>updated</h1>" {
		t.Errorf("unexpected content: %s", f.Content)
	}
}

func TestClientUpdateNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	_, err := c.Update("nonexistent", map[string][]byte{"f.html": []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*gist.NotFoundError); !ok {
		t.Fatalf("expected *NotFoundError, got %T", err)
	}
}

func TestClientDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/gists/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	err := c.Delete("abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientDeleteNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := gist.NewClient(&http.Client{})
	c.BaseURL = server.URL

	err := c.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*gist.NotFoundError); !ok {
		t.Fatalf("expected *NotFoundError, got %T", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/gist/ -v
```

Expected: compilation errors — `List`, `Update`, `Delete` not defined.

- [ ] **Step 3: Implement List, Update, Delete**

Add to `internal/gist/client.go` (add `"strings"` to imports):

```go
func (c *Client) List() ([]Gist, error) {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/gists?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("listing gists: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var apiGists []apiGist
	if err := json.NewDecoder(resp.Body).Decode(&apiGists); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	var gists []Gist
	for _, ag := range apiGists {
		if strings.HasPrefix(ag.Description, DescriptionPrefix) {
			gists = append(gists, ag.toGist())
		}
	}

	return gists, nil
}

func (c *Client) Update(gistID string, files map[string][]byte) (Gist, error) {
	apiFiles := make(map[string]interface{}, len(files))
	for name, content := range files {
		apiFiles[name] = map[string]string{
			"content": string(content),
		}
	}

	body := map[string]interface{}{
		"files": apiFiles,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return Gist{}, fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, c.BaseURL+"/gists/"+gistID, &buf)
	if err != nil {
		return Gist{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Gist{}, fmt.Errorf("updating gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Gist{}, &NotFoundError{GistID: gistID}
	}
	if resp.StatusCode != http.StatusOK {
		return Gist{}, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var ag apiGist
	if err := json.NewDecoder(resp.Body).Decode(&ag); err != nil {
		return Gist{}, fmt.Errorf("decoding response: %w", err)
	}

	return ag.toGist(), nil
}

func (c *Client) Delete(gistID string) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+"/gists/"+gistID, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{GistID: gistID}
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}
```

- [ ] **Step 4: Run all tests to verify they pass**

```bash
go test ./internal/gist/ -v
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gist/client.go internal/gist/client_test.go
git commit -m "feat: add gist List, Update, Delete methods"
```

---

### Task 5: Proxy Handler

**Files:**
- Create: `internal/proxy/handler.go`
- Create: `internal/proxy/handler_test.go`

- [ ] **Step 1: Write failing tests for the proxy handler**

Create `internal/proxy/handler_test.go`:

```go
package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kroepke/gh-htmlgist/internal/gist"
	"github.com/kroepke/gh-htmlgist/internal/proxy"
)

type mockFetcher struct {
	gists map[string]gist.Gist
}

func (m *mockFetcher) Get(gistID string) (gist.Gist, error) {
	g, ok := m.gists[gistID]
	if !ok {
		return gist.Gist{}, &gist.NotFoundError{GistID: gistID}
	}
	return g, nil
}

func newTestHandler() (*proxy.Handler, *mockFetcher) {
	fetcher := &mockFetcher{
		gists: map[string]gist.Gist{
			"abc123": {
				ID:          "abc123",
				Description: "[htmlgist] test",
				Files: map[string]gist.File{
					"index.html": {
						Filename: "index.html",
						Content:  "<h1>hello</h1>",
						Size:     14,
						Type:     "text/html",
					},
					"style.css": {
						Filename: "style.css",
						Content:  "h1 { color: red; }",
						Size:     19,
						Type:     "text/css",
					},
				},
			},
			"nohtml": {
				ID:          "nohtml",
				Description: "[htmlgist] no html",
				Files: map[string]gist.File{
					"data.json": {
						Filename: "data.json",
						Content:  `{"key":"value"}`,
						Size:     15,
						Type:     "application/json",
					},
				},
			},
		},
	}
	return proxy.NewHandler(fetcher), fetcher
}

func TestHealthCheck(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGistIDRedirectsToTrailingSlash(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/abc123/" {
		t.Errorf("expected redirect to /abc123/, got %s", w.Header().Get("Location"))
	}
}

func TestServesFirstHTMLFile(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/abc123/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "<h1>hello</h1>" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" && ct != "text/html" {
		t.Errorf("unexpected content-type: %s", ct)
	}
}

func TestServesNamedFile(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/abc123/style.css", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "h1 { color: red; }" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/css; charset=utf-8" && ct != "text/css" {
		t.Errorf("unexpected content-type: %s", ct)
	}
}

func TestGistNotFound(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestNoHTMLFileInGist(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/nohtml/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestFileNotFoundInGist(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/abc123/missing.js", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestRootPathReturns404(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/proxy/ -v
```

Expected: compilation error — `proxy` package not found.

- [ ] **Step 3: Implement the proxy handler**

Create `internal/proxy/handler.go`:

```go
package proxy

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/kroepke/gh-htmlgist/internal/gist"
)

type GistFetcher interface {
	Get(gistID string) (gist.Gist, error)
}

type Handler struct {
	fetcher GistFetcher
}

func NewHandler(fetcher GistFetcher) *Handler {
	return &Handler{fetcher: fetcher}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	if path == "health" || path == "health/" {
		fmt.Fprint(w, "ok")
		return
	}

	parts := strings.SplitN(path, "/", 2)
	gistID := parts[0]

	if gistID == "" {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 1 {
		http.Redirect(w, r, "/"+gistID+"/", http.StatusMovedPermanently)
		return
	}

	g, err := h.fetcher.Get(gistID)
	if err != nil {
		var nfe *gist.NotFoundError
		if errors.As(err, &nfe) {
			http.Error(w, "gist not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal server error", http.StatusBadGateway)
		}
		return
	}

	filename := parts[1]

	if filename == "" {
		for _, f := range g.Files {
			ext := strings.ToLower(filepath.Ext(f.Filename))
			if ext == ".html" || ext == ".htm" {
				filename = f.Filename
				break
			}
		}
		if filename == "" {
			http.Error(w, "no HTML file found in this gist", http.StatusNotFound)
			return
		}
	}

	file, ok := g.Files[filename]
	if !ok {
		http.NotFound(w, r)
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(filename))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Write([]byte(file.Content))
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/proxy/ -v
```

Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/handler.go internal/proxy/handler_test.go
git commit -m "feat: add proxy handler with routing and MIME detection"
```

---

### Task 6: Proxy Server Binary

**Files:**
- Create: `cmd/htmlgist-proxy/main.go`

- [ ] **Step 1: Implement the proxy entrypoint**

Create `cmd/htmlgist-proxy/main.go`:

```go
package main

import (
	"log"
	"net/http"
	"os"

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

	addr := os.Getenv("HTMLGIST_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	httpClient := &http.Client{
		Transport: &tokenTransport{
			token:   token,
			wrapped: http.DefaultTransport,
		},
	}

	client := gist.NewClient(httpClient)
	handler := proxy.NewHandler(client)

	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/htmlgist-proxy/
```

Expected: no output, exit 0. Binary `htmlgist-proxy` created in current directory.

- [ ] **Step 3: Clean up and commit**

```bash
rm -f htmlgist-proxy
git add cmd/htmlgist-proxy/main.go
git commit -m "feat: add proxy server binary"
```

---

### Task 7: CLI — Foundation & Setup

**Files:**
- Create: `cmd/gh-htmlgist/main.go`

This task requires the `go-gh` dependency.

- [ ] **Step 1: Add the go-gh dependency**

```bash
go get github.com/cli/go-gh/v2
```

- [ ] **Step 2: Implement CLI with setup command**

Create `cmd/gh-htmlgist/main.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	ghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/kroepke/gh-htmlgist/internal/gist"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "setup":
		err = runSetup()
	case "create":
		err = runCreate(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "list":
		err = runList()
	case "delete":
		err = runDelete(os.Args[2:])
	case "open":
		err = runOpen(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: gh htmlgist <command> [arguments]

Commands:
  setup              Configure the proxy URL
  create <file...>   Create a new gist from files
  update <id> <file...>  Update an existing gist
  list               List your htmlgist gists
  delete <id>        Delete a gist
  open <id>          Open a gist in the browser
`)
}

func getProxyURL() string {
	out, err := exec.Command("gh", "config", "get", "extensions.htmlgist.proxy-url").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func setProxyURL(url string) error {
	return exec.Command("gh", "config", "set", "extensions.htmlgist.proxy-url", url).Run()
}

func ensureSetup() error {
	if getProxyURL() != "" {
		return nil
	}
	fmt.Println("No proxy URL configured. Running setup...")
	return runSetup()
}

func runSetup() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("Enter your htmlgist proxy URL (e.g. https://gist.internal.example.com): ")
	if !scanner.Scan() {
		return fmt.Errorf("no input received")
	}
	url := strings.TrimSpace(scanner.Text())
	url = strings.TrimRight(url, "/")

	if url == "" {
		return fmt.Errorf("proxy URL cannot be empty")
	}

	fmt.Printf("Checking health endpoint at %s/health...\n", url)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not reach proxy at %s/health: %v\n", url, err)
		fmt.Print("Save this URL anyway? [y/N]: ")
		if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			return fmt.Errorf("setup cancelled")
		}
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Warning: health check returned status %d\n", resp.StatusCode)
			fmt.Print("Save this URL anyway? [y/N]: ")
			if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
				return fmt.Errorf("setup cancelled")
			}
		} else {
			fmt.Println("Health check passed!")
		}
	}

	if err := setProxyURL(url); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Proxy URL saved: %s\n", url)
	return nil
}

func newGistClient() (*gist.Client, error) {
	httpClient, err := ghapi.NewHTTPClient(ghapi.ClientOptions{})
	if err != nil {
		return nil, fmt.Errorf("creating GitHub client: %w", err)
	}
	return gist.NewClient(httpClient), nil
}

func printGistInfo(g gist.Gist) {
	proxyURL := getProxyURL()
	fmt.Printf("Gist ID:    %s\n", g.ID)
	fmt.Printf("GitHub URL: %s\n", g.HTMLURL)
	if proxyURL != "" {
		fmt.Printf("Proxy URL:  %s/%s/\n", proxyURL, g.ID)
	}
}

// Placeholder — implemented in Task 8
func runCreate(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runUpdate(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runList() error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 9
func runDelete(args []string) error {
	return fmt.Errorf("not yet implemented")
}

// Placeholder — implemented in Task 8
func runOpen(args []string) error {
	return fmt.Errorf("not yet implemented")
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./cmd/gh-htmlgist/
```

Expected: no output, exit 0.

- [ ] **Step 4: Clean up and commit**

```bash
rm -f gh-htmlgist
git add cmd/gh-htmlgist/main.go go.mod go.sum
git commit -m "feat: add CLI skeleton with setup command"
```

---

### Task 8: CLI — Create & Open Commands

**Files:**
- Modify: `cmd/gh-htmlgist/main.go`

- [ ] **Step 1: Implement create and open commands**

Replace the `runCreate` and `runOpen` placeholders in `cmd/gh-htmlgist/main.go`:

```go
func runCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gh htmlgist create <file...>")
	}

	if err := ensureSetup(); err != nil {
		return err
	}

	files := make(map[string][]byte, len(args))
	for _, path := range args {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		name := filepath.Base(path)
		files[name] = content
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	desc := ""
	for name := range files {
		if strings.HasSuffix(strings.ToLower(name), ".html") || strings.HasSuffix(strings.ToLower(name), ".htm") {
			desc = name
			break
		}
	}
	if desc == "" {
		for name := range files {
			desc = name
			break
		}
	}

	g, err := client.Create(files, desc)
	if err != nil {
		return fmt.Errorf("creating gist: %w", err)
	}

	printGistInfo(g)
	return nil
}

func runOpen(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gh htmlgist open <gist-id>")
	}

	proxyURL := getProxyURL()
	if proxyURL == "" {
		return fmt.Errorf("no proxy URL configured — run 'gh htmlgist setup' first")
	}

	url := proxyURL + "/" + args[0] + "/"

	b := browser.New("", os.Stdout, os.Stderr)
	return b.Browse(url)
}
```

Add `"path/filepath"` and `"github.com/cli/go-gh/v2/pkg/browser"` to the import block.

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/gh-htmlgist/
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
rm -f gh-htmlgist
git add cmd/gh-htmlgist/main.go
git commit -m "feat: add create and open commands"
```

---

### Task 9: CLI — List, Update, Delete Commands

**Files:**
- Modify: `cmd/gh-htmlgist/main.go`

- [ ] **Step 1: Implement list, update, and delete commands**

Replace the `runList`, `runUpdate`, and `runDelete` placeholders in `cmd/gh-htmlgist/main.go`:

```go
func runList() error {
	client, err := newGistClient()
	if err != nil {
		return err
	}

	gists, err := client.List()
	if err != nil {
		return fmt.Errorf("listing gists: %w", err)
	}

	if len(gists) == 0 {
		fmt.Println("No htmlgist gists found.")
		return nil
	}

	proxyURL := getProxyURL()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tDESCRIPTION\tFILES\tUPDATED\n")
	for _, g := range gists {
		desc := strings.TrimPrefix(g.Description, gist.DescriptionPrefix)
		fileNames := make([]string, 0, len(g.Files))
		for name := range g.Files {
			fileNames = append(fileNames, name)
		}
		updated := g.UpdatedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", g.ID, desc, strings.Join(fileNames, ", "), updated)
	}
	tw.Flush()

	if proxyURL != "" {
		fmt.Printf("\nProxy: %s/<gist-id>/\n", proxyURL)
	}

	return nil
}

func runUpdate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gh htmlgist update <gist-id> <file...>")
	}

	gistID := args[0]
	filePaths := args[1:]

	files := make(map[string][]byte, len(filePaths))
	for _, path := range filePaths {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		name := filepath.Base(path)
		files[name] = content
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	g, err := client.Update(gistID, files)
	if err != nil {
		return fmt.Errorf("updating gist: %w", err)
	}

	fmt.Println("Gist updated.")
	printGistInfo(g)
	return nil
}

func runDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: gh htmlgist delete <gist-id>")
	}

	client, err := newGistClient()
	if err != nil {
		return err
	}

	if err := client.Delete(args[0]); err != nil {
		return fmt.Errorf("deleting gist: %w", err)
	}

	fmt.Printf("Gist %s deleted.\n", args[0])
	return nil
}
```

- [ ] **Step 2: Remove unused import and verify compilation**

Make sure `"text/tabwriter"` is in the import block (it was added in the initial skeleton but now actually used). Verify:

```bash
go build ./cmd/gh-htmlgist/
```

Expected: no output, exit 0.

- [ ] **Step 3: Run all tests**

```bash
go test ./... -v
```

Expected: all gist client and proxy handler tests PASS.

- [ ] **Step 4: Commit**

```bash
rm -f gh-htmlgist
git add cmd/gh-htmlgist/main.go
git commit -m "feat: add list, update, delete commands"
```

---

### Task 10: Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Create the Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /htmlgist-proxy ./cmd/htmlgist-proxy/

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
COPY --from=build /htmlgist-proxy /usr/local/bin/htmlgist-proxy

EXPOSE 8080
ENTRYPOINT ["htmlgist-proxy"]
```

- [ ] **Step 2: Verify it builds (optional — requires Docker)**

```bash
docker build -t htmlgist-proxy .
```

Expected: image builds successfully.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat: add Dockerfile for proxy"
```

---

### Task 11: GoReleaser & CI

**Files:**
- Create: `.goreleaser.yml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create GoReleaser config**

Create `.goreleaser.yml`:

```yaml
version: 2

builds:
  - id: gh-htmlgist
    main: ./cmd/gh-htmlgist
    binary: gh-htmlgist
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64

  - id: htmlgist-proxy
    main: ./cmd/htmlgist-proxy
    binary: htmlgist-proxy
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64

archives:
  - id: cli
    builds:
      - gh-htmlgist
    name_template: "gh-htmlgist-{{ .Os }}-{{ .Arch }}"
    format: binary

  - id: proxy
    builds:
      - htmlgist-proxy
    name_template: "htmlgist-proxy-{{ .Os }}-{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
```

Note: the CLI archive uses `format: binary` (no wrapping archive) because `gh extension install` expects bare binaries named `gh-htmlgist-<os>-<arch>` in the release assets.

- [ ] **Step 2: Create CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test ./... -v -race
```

- [ ] **Step 3: Create release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 4: Verify everything compiles and tests pass**

```bash
go build ./...
go test ./... -v
```

Expected: both binaries compile, all tests pass.

- [ ] **Step 5: Commit**

```bash
mkdir -p .github/workflows
git add .goreleaser.yml .github/workflows/ci.yml .github/workflows/release.yml
git commit -m "feat: add GoReleaser config and CI pipelines"
```
