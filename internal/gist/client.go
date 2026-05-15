package gist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	apiFiles := make(map[string]any, len(files))
	for name, content := range files {
		apiFiles[name] = map[string]string{
			"content": string(content),
		}
	}

	body := map[string]any{
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

func (c *Client) Create(files map[string][]byte, description string) (Gist, error) {
	apiFiles := make(map[string]any, len(files))
	for name, content := range files {
		apiFiles[name] = map[string]string{
			"content": string(content),
		}
	}

	body := map[string]any{
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
