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
