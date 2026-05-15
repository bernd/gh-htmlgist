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
