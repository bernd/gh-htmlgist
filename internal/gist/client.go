package gist

import (
	"bytes"
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
