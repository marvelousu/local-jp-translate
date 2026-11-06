package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompleteSendsOfficialRequest(t *testing.T) {
	var got struct {
		Prompt      string   `json:"prompt"`
		Temperature float64  `json:"temperature"`
		MaxTokens   int      `json:"max_tokens"`
		Stop        []string `json:"stop"`
		Stream      bool     `json:"stream"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"text":"こんにちは\n","finish_reason":"stop"}],"usage":{"completion_tokens":3}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	result, err := client.Complete(context.Background(), CompletionRequest{
		Prompt:    "<|plamo:tag|>dataset<|plamo:msg|>translation",
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Text != "こんにちは" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.CompletionTokens != 3 {
		t.Fatalf("completion tokens = %d", result.CompletionTokens)
	}
	if got.Temperature != 0 {
		t.Fatalf("temperature = %v", got.Temperature)
	}
	if got.MaxTokens != 64 || got.Stream {
		t.Fatalf("unexpected request: %+v", got)
	}
	if len(got.Stop) != 2 || got.Stop[0] != "<|plamo:tag|>" || got.Stop[1] != "<|plamo:bos|>" {
		t.Fatalf("stop = %#v", got.Stop)
	}
	if strings.Contains(got.Prompt, "<|plamo:bos|>") {
		t.Fatal("client must not prepend BOS")
	}
}

func TestCompleteRejectsBadUpstreamOutput(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"truncated", `{"choices":[{"text":"partial","finish_reason":"length"}]}`, ErrTruncated},
		{"empty", `{"choices":[{"text":"  \n","finish_reason":"stop"}]}`, ErrEmpty},
		{"missing finish", `{"choices":[{"text":"x","finish_reason":""}]}`, ErrIncomplete},
		{"wrong finish", `{"choices":[{"text":"x","finish_reason":"content_filter"}]}`, ErrIncomplete},
		{"no choices", `{"choices":[]}`, ErrMalformed},
		{"malformed", `{not json`, ErrMalformed},
		{"unsafe marker", `{"choices":[{"text":"a<|plamo:msg|>b","finish_reason":"stop"}]}`, ErrUnsafeOutput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Complete(context.Background(), CompletionRequest{Prompt: "x", MaxTokens: 8})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCompleteStripsTrailingStopMarkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"text":"訳文\n<|plamo:tag|>","finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Complete(context.Background(), CompletionRequest{Prompt: "x", MaxTokens: 8})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Text != "訳文" {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestCompleteHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"model not loaded"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), CompletionRequest{Prompt: "x", MaxTokens: 8})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if httpErr.StatusCode != 500 || !strings.Contains(httpErr.Body, "model not loaded") {
		t.Fatalf("unexpected error: %+v", httpErr)
	}
}

func TestCompleteHonorsContextTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = client.Complete(ctx, CompletionRequest{Prompt: "x", MaxTokens: 8})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout was not honored promptly")
	}
}

func TestHealth(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := client.Health(context.Background()); err == nil {
		t.Fatal("expected health failure")
	}
}

func TestLoopbackValidation(t *testing.T) {
	valid := []string{"http://127.0.0.1:8000", "http://localhost:8000", "http://[::1]:8000", "https://127.0.0.1:8443"}
	for _, raw := range valid {
		if _, err := ParseLoopbackURL(raw); err != nil {
			t.Errorf("ParseLoopbackURL(%q): %v", raw, err)
		}
	}
	invalid := []string{
		"",
		"http://example.com:8000",
		"http://0.0.0.0:8000",
		"http://192.168.1.2:8000",
		"http://user:pass@127.0.0.1:8000",
		"http://127.0.0.1:8000/v1",
		"http://127.0.0.1:8000?x=1",
		"ftp://127.0.0.1:8000",
	}
	for _, raw := range invalid {
		if _, err := ParseLoopbackURL(raw); err == nil {
			t.Errorf("ParseLoopbackURL(%q) should fail", raw)
		}
	}

	if err := ValidateListenAddress("127.0.0.1:8787"); err != nil {
		t.Errorf("valid listen address rejected: %v", err)
	}
	if err := ValidateListenAddress("[::1]:8787"); err != nil {
		t.Errorf("valid IPv6 listen address rejected: %v", err)
	}
	for _, raw := range []string{"0.0.0.0:8787", ":8787", "192.168.1.4:8787", "127.0.0.1"} {
		if err := ValidateListenAddress(raw); err == nil {
			t.Errorf("ValidateListenAddress(%q) should fail", raw)
		}
	}
}
