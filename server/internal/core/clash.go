package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// clashClient talks to a running sing-box through its Clash API
// (experimental.clash_api), which the official builds include.
type clashClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewClashClient returns a CoreClient backed by the Clash API at baseURL
// (e.g. http://127.0.0.1:9090). secret may be empty.
func NewClashClient(baseURL, secret string) CoreClient {
	return &clashClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *clashClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("core: clash api %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core: clash api %s: unexpected status %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *clashClient) Version(ctx context.Context) (string, error) {
	var body struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/version", &body); err != nil {
		return "", err
	}
	if body.Version == "" {
		return "", fmt.Errorf("core: clash api /version: empty version")
	}
	return body.Version, nil
}

func (c *clashClient) Totals(ctx context.Context) (Totals, error) {
	var body struct {
		UploadTotal   int64             `json:"uploadTotal"`
		DownloadTotal int64             `json:"downloadTotal"`
		Connections   []json.RawMessage `json:"connections"`
	}
	if err := c.get(ctx, "/connections", &body); err != nil {
		return Totals{}, err
	}
	return Totals{
		UploadTotal:   body.UploadTotal,
		DownloadTotal: body.DownloadTotal,
		ActiveConns:   len(body.Connections),
	}, nil
}
