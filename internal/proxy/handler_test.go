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
