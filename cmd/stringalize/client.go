package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the public Stringalize API with a project API key.
type Client struct {
	server string
	key    string
	http   *http.Client
}

type Locale struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Source   bool     `json:"source"`
	Progress Progress `json:"progress"`
}

type Progress struct {
	Translated   int `json:"translated"`
	Untranslated int `json:"untranslated"`
	Flagged      int `json:"flagged"`
}

type Project struct {
	Project struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"project"`
	Key struct {
		Name   string `json:"name"`
		Access string `json:"access"`
	} `json:"key"`
}

type ImportResult struct {
	Message  string   `json:"message"`
	Warnings []string `json:"warnings"`
}

func newClient(server, key string) *Client {
	return &Client{server: strings.TrimRight(server, "/"), key: key, http: &http.Client{Timeout: 2 * time.Minute}}
}

func (c *Client) Verify() (*Project, error) {
	var project Project
	return &project, c.getJSON("/api/auth/verify", &project)
}

// Locales returns the project's locales, the source locale first.
func (c *Client) Locales() ([]Locale, error) {
	var locales []Locale
	if err := c.getJSON("/api/locales", &locales); err != nil {
		return nil, err
	}
	if sourceLocale(locales) == nil {
		return nil, fmt.Errorf("the project has no source locale")
	}
	return locales, nil
}

// Problems returns how many strings of a locale fail validation (placeholders, plural forms).
func (c *Client) Problems(code string) (int, error) {
	var problems []json.RawMessage
	err := c.getJSON("/api/locales/"+url.PathEscape(code)+"/errors", &problems)
	return len(problems), err
}

func (c *Client) Export(code, format string, query url.Values) ([]byte, error) {
	return c.request(http.MethodGet, "/api/export/locale/"+url.PathEscape(code)+"."+format, query, nil)
}

func (c *Client) ExportAll(format string, query url.Values) ([]byte, error) {
	return c.request(http.MethodGet, "/api/export/all."+format, query, nil)
}

func (c *Client) Import(format string, query url.Values, body []byte) (*ImportResult, error) {
	data, err := c.request(http.MethodPost, "/api/import/"+format, query, body)
	if err != nil {
		return nil, err
	}
	var result ImportResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unexpected import response: %w", err)
	}
	return &result, nil
}

func (c *Client) getJSON(path string, target any) error {
	data, err := c.request(http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unexpected response from %s: %w", path, err)
	}
	return nil
}

func (c *Client) request(method, path string, query url.Values, body []byte) ([]byte, error) {
	address := c.server + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, address, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Loco "+c.key)
	request.Header.Set("User-Agent", "stringalize-cli/"+version)
	if body != nil {
		request.Header.Set("Content-Type", "application/octet-stream")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.server, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &failure) == nil && failure.Error != "" {
			return nil, fmt.Errorf("%s (HTTP %d)", failure.Error, response.StatusCode)
		}
		return nil, fmt.Errorf("%s %s answered HTTP %d", method, path, response.StatusCode)
	}
	return data, nil
}

func sourceLocale(locales []Locale) *Locale {
	for i := range locales {
		if locales[i].Source {
			return &locales[i]
		}
	}
	return nil
}
