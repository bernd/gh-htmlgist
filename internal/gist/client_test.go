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
