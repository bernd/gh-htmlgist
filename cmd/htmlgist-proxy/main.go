package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/kroepke/gh-htmlgist/internal/auth"
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
	var handler http.Handler = proxy.NewHandler(client)

	if os.Getenv("HTMLGIST_AUTH_DISABLED") != "true" {
		cfTeamURL := os.Getenv("HTMLGIST_CF_TEAM_URL")
		if cfTeamURL == "" {
			log.Fatal("HTMLGIST_CF_TEAM_URL environment variable is required")
		}

		cfAudience := os.Getenv("HTMLGIST_CF_AUDIENCE")
		if cfAudience == "" {
			log.Fatal("HTMLGIST_CF_AUDIENCE environment variable is required")
		}

		keyStore := auth.NewKeyStore(cfTeamURL)
		if err := keyStore.Refresh(); err != nil {
			slog.Error("failed to fetch initial JWKS", "error", err)
			os.Exit(1)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go keyStore.StartBackgroundRefresh(ctx, 5*time.Minute)

		validator := auth.NewValidator(keyStore, cfTeamURL, cfAudience)
		handler = auth.Middleware(validator, handler)
	} else {
		slog.Warn("authentication is disabled")
	}

	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
