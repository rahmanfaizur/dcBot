// Package ai provides a thin Groq chat client for Discord slash commands.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultModel = "qwen/qwen3.8-27b"
	apiURL       = "https://api.groq.com/openai/v1/chat/completions"
	maxTokens    = 400
	cooldown     = 10 * time.Second
)

// Client talks to Groq's OpenAI-compatible chat API.
type Client struct {
	apiKey string
	model  string
	http   *http.Client

	mu       sync.Mutex
	lastByID map[string]time.Time
}

// New returns a client when apiKey is set; otherwise nil (AI disabled).
func New(apiKey, model string) *Client {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}
	return &Client{
		apiKey:   apiKey,
		model:    model,
		http:     &http.Client{Timeout: 25 * time.Second},
		lastByID: make(map[string]time.Time),
	}
}

// Enabled reports whether AI features are available.
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

// Allow enforces a simple per-user cooldown. Returns remaining wait if blocked.
func (c *Client) Allow(userID string) (ok bool, wait time.Duration) {
	if !c.Enabled() {
		return false, 0
	}
	if userID == "" {
		return true, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, hit := c.lastByID[userID]; hit {
		if rem := cooldown - time.Since(last); rem > 0 {
			return false, rem.Round(time.Second)
		}
	}
	c.lastByID[userID] = time.Now()
	return true, 0
}

// Chat sends a system + user message and returns the assistant reply.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("AI is not configured")
	}
	system = strings.TrimSpace(system)
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("empty prompt")
	}

	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.7,
		"max_tokens":  maxTokens,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("groq HTTP %d: %s", res.StatusCode, truncate(string(raw), 200))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty AI response")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		text = strings.TrimSpace(parsed.Choices[0].Message.Reasoning)
	}
	if text == "" {
		return "", fmt.Errorf("empty AI response")
	}
	return truncateDiscord(text), nil
}

// PolishFact turns a Wikipedia/raw fact into a short Discord-friendly line.
func (c *Client) PolishFact(ctx context.Context, artist, title, raw string) (string, error) {
	system := "You polish song trivia for a Discord music bot. Reply with ONE short fun fact (max 2 sentences). No preamble, no hashtags."
	user := fmt.Sprintf("Artist: %s\nTitle: %s\nSource: %s", artist, title, raw)
	return c.Chat(ctx, system, user)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func truncateDiscord(s string) string {
	const limit = 1900
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
