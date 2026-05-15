package main

import (
	"log"
	"net/http"
	"os"
	"time"

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
		Timeout: 10 * time.Second,
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
