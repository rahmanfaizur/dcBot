package linkdave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RESTClient calls Linkdave HTTP endpoints for a single node session.
type RESTClient struct {
	baseURL    string
	password   string
	httpClient *http.Client
}

// NewRESTClient builds a REST client from a WebSocket node URL.
func NewRESTClient(wsURL, password string) (*RESTClient, error) {
	baseURL, err := wsToHTTPBase(wsURL)
	if err != nil {
		return nil, err
	}

	return &RESTClient{
		baseURL:  baseURL,
		password: password,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

func wsToHTTPBase(wsURL string) (string, error) {
	switch {
	case strings.HasPrefix(wsURL, "wss://"):
		return "https://" + strings.TrimPrefix(wsURL, "wss://"), nil
	case strings.HasPrefix(wsURL, "ws://"):
		return "http://" + strings.TrimPrefix(wsURL, "ws://"), nil
	default:
		return "", fmt.Errorf("invalid linkdave url %q: must start with ws:// or wss://", wsURL)
	}
}

func (c *RESTClient) play(ctx context.Context, sessionID, guildID string, req PlayRequest) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/sessions/%s/players/%s/play", sessionID, guildID), req)
}

func (c *RESTClient) pause(ctx context.Context, sessionID, guildID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/sessions/%s/players/%s/pause", sessionID, guildID), nil)
}

func (c *RESTClient) resume(ctx context.Context, sessionID, guildID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/sessions/%s/players/%s/resume", sessionID, guildID), nil)
}

func (c *RESTClient) stop(ctx context.Context, sessionID, guildID string) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/sessions/%s/players/%s/stop", sessionID, guildID), nil)
}

func (c *RESTClient) disconnect(ctx context.Context, sessionID, guildID string) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/sessions/%s/players/%s", sessionID, guildID), nil)
}

func (c *RESTClient) do(ctx context.Context, method, path string, body any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.password != "" {
		req.Header.Set("Authorization", "Bearer "+c.password)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return fmt.Errorf("linkdave %s %s: %s", method, path, strings.TrimSpace(string(msg)))
}
