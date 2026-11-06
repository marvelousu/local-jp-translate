// Package backend talks to a loopback llama-server and manages its lifecycle.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/marvelousu/local-jp-translate/internal/plamo"
)

// Sentinel errors for upstream results that must not be reported as success.
var (
	ErrTruncated    = errors.New("upstream generation hit the token limit")
	ErrEmpty        = errors.New("upstream returned an empty translation")
	ErrMalformed    = errors.New("upstream returned a malformed response")
	ErrIncomplete   = errors.New("upstream did not finish with a stop reason")
	ErrUnsafeOutput = errors.New("upstream output contains a reserved PFML marker")
)

const maxResponseBytes = 4 << 20

// HTTPError is a non-200 response from llama-server.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("llama-server returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("llama-server returned HTTP %d: %s", e.StatusCode, e.Body)
}

// CompletionRequest is one raw PFML3 completion.
type CompletionRequest struct {
	Prompt    string
	MaxTokens int
	Model     string
}

// CompletionResult is a validated completion.
type CompletionResult struct {
	Text             string
	CompletionTokens int
}

// Client is a llama-server /v1/completions client.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a client for a loopback base URL such as
// http://127.0.0.1:8000.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	u, err := ParseLoopbackURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy:                 nil,
				MaxIdleConns:          4,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 0,
			},
		}
	}
	localClient := *httpClient
	localClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(u.String(), "/"), http: &localClient}, nil
}

// BaseURL returns the normalized backend base URL.
func (c *Client) BaseURL() string { return c.baseURL }

type completionsRequest struct {
	Prompt      string   `json:"prompt"`
	Temperature float64  `json:"temperature"`
	MaxTokens   int      `json:"max_tokens"`
	Stop        []string `json:"stop"`
	Stream      bool     `json:"stream"`
	Model       string   `json:"model,omitempty"`
}

type completionsResponse struct {
	Choices []struct {
		Text         string `json:"text"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// Complete sends a raw PFML3 prompt to /v1/completions with temperature 0 and
// the official stop markers. It rejects truncated, empty, malformed, and
// marker-containing output instead of returning a partial translation.
func (c *Client) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return CompletionResult{}, errors.New("prompt must not be empty")
	}
	if req.MaxTokens <= 0 {
		return CompletionResult{}, errors.New("max_tokens must be positive")
	}

	payload, err := json.Marshal(completionsRequest{
		Prompt:      req.Prompt,
		Temperature: 0,
		MaxTokens:   req.MaxTokens,
		Stop:        plamo.StopMarkers,
		Stream:      false,
		Model:       req.Model,
	})
	if err != nil {
		return CompletionResult{}, fmt.Errorf("encode completion request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/completions", bytes.NewReader(payload))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("create completion request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("read llama-server response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return CompletionResult{}, ErrMalformed
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResult{}, &HTTPError{StatusCode: resp.StatusCode, Body: extractUpstreamError(body)}
	}

	var decoded completionsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return CompletionResult{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if decoded.Error != nil {
		return CompletionResult{}, &HTTPError{StatusCode: resp.StatusCode, Body: decoded.Error.Message}
	}
	if len(decoded.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("%w: no choices", ErrMalformed)
	}

	choice := decoded.Choices[0]
	switch choice.FinishReason {
	case "stop":
	case "length":
		return CompletionResult{}, ErrTruncated
	case "":
		return CompletionResult{}, fmt.Errorf("%w: missing finish_reason", ErrIncomplete)
	default:
		return CompletionResult{}, fmt.Errorf("%w: finish_reason=%s", ErrIncomplete, choice.FinishReason)
	}

	text, err := cleanOutput(choice.Text)
	if err != nil {
		return CompletionResult{}, err
	}
	return CompletionResult{Text: text, CompletionTokens: decoded.Usage.CompletionTokens}, nil
}

// Health checks llama-server's /health endpoint. A 200 means the model is
// loaded; 503 is normal while llama-server is still starting.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llama-server health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func extractUpstreamError(body []byte) string {
	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &decoded) == nil {
		if decoded.Error.Message != "" {
			return truncate(decoded.Error.Message, 500)
		}
		if decoded.Message != "" {
			return truncate(decoded.Message, 500)
		}
	}
	return truncate(strings.TrimSpace(string(body)), 500)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func cleanOutput(text string) (string, error) {
	cleaned := text
	for {
		changed := false
		suffix := strings.TrimRightFunc(cleaned, unicode.IsSpace)
		for _, stop := range plamo.StopMarkers {
			if strings.HasSuffix(suffix, stop) {
				cleaned = strings.TrimSuffix(suffix, stop)
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	if strings.TrimSpace(cleaned) == "" {
		return "", ErrEmpty
	}
	if strings.Contains(cleaned, plamo.Reserved) {
		return "", ErrUnsafeOutput
	}
	return cleaned, nil
}

// ParseLoopbackURL validates that raw points at http(s) on a loopback host and
// has no credentials, query, or fragment.
func ParseLoopbackURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("backend URL is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("backend URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("backend URL must not contain credentials")
	}
	if u.Host == "" {
		return nil, errors.New("backend URL must include a host and port")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("backend URL must not contain a query or fragment")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("backend URL must not contain a path")
	}
	if err := ValidateLoopbackHost(u.Hostname()); err != nil {
		return nil, fmt.Errorf("backend URL: %w", err)
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

// ValidateLoopbackHost accepts localhost, 127.0.0.0/8, and ::1.
func ValidateLoopbackHost(host string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return nil
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return fmt.Errorf("host %q is not a loopback address", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("host %q is not a loopback address", host)
	}
	return nil
}

// ValidateListenAddress requires an explicit loopback host and a port.
func ValidateListenAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("listen address %q needs a port", addr)
	}
	if host == "" {
		return fmt.Errorf("listen address %q must name a loopback host", addr)
	}
	if err := ValidateLoopbackHost(host); err != nil {
		return fmt.Errorf("listen address: %w", err)
	}
	return nil
}
