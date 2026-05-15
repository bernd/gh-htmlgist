package proxy

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
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
		if f, ok := g.Files["index.html"]; ok {
			filename = f.Filename
		} else {
			var htmlFiles []string
			for _, f := range g.Files {
				ext := strings.ToLower(filepath.Ext(f.Filename))
				if ext == ".html" || ext == ".htm" {
					htmlFiles = append(htmlFiles, f.Filename)
				}
			}
			sort.Strings(htmlFiles)
			if len(htmlFiles) > 0 {
				filename = htmlFiles[0]
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
