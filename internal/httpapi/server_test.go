package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marvelousu/local-jp-translate/internal/backend"
	"github.com/marvelousu/local-jp-translate/internal/translate"
)

type fakeBackend struct {
	text      string
	err       error
	healthErr error
	last      backend.CompletionRequest
	calls     int
}

func (f *fakeBackend) Complete(ctx context.Context, req backend.CompletionRequest) (backend.CompletionResult, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return backend.CompletionResult{}, f.err
	}
	text := f.text
	if text == "" {
		text = "こんにちは"
	}
	return backend.CompletionResult{Text: text}, nil
}

func (f *fakeBackend) Health(ctx context.Context) error { return f.healthErr }

func newAPI(t *testing.T, fake *fakeBackend, opts ...func(*Options)) *httptest.Server {
	t.Helper()
	svc := translate.NewService(translate.Config{Model: "test-model"}, fake)
	options := Options{Service: svc, Version: "test", MaxBody: 1 << 20}
	for _, opt := range opts {
		opt(&options)
	}
	server := httptest.NewServer(New(options))
	t.Cleanup(server.Close)
	return server
}

func postJSON(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, data
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, data []byte) errorEnvelope {
	t.Helper()
	var envelope errorEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode error body %q: %v", data, err)
	}
	return envelope
}

func TestTranslateSuccess(t *testing.T) {
	fake := &fakeBackend{text: "こんにちは"}
	server := newAPI(t, fake)
	resp, body := postJSON(t, server.URL+"/translate", `{
		"text":"Hello.",
		"source":"en",
		"target":"ja",
		"style":"polite",
		"glossary":[{"source":"Hello","target":"こんにちは"}],
		"context":"A greeting.",
		"max_tokens":256
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var decoded translateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.TranslatedText != "こんにちは" || decoded.TranslatedTextCamel != "こんにちは" {
		t.Fatalf("aliases missing: %+v", decoded)
	}
	if decoded.Model != "test-model" {
		t.Fatalf("model = %s", decoded.Model)
	}
	if decoded.DurationMS < 0 {
		t.Fatalf("duration = %d", decoded.DurationMS)
	}
	prompt := fake.last.Prompt
	for _, want := range []string{
		"<|plamo:tag|>context<|plamo:msg|>A greeting.",
		"<|plamo:index|>Hello<|plamo:item|>こんにちは",
		"<|plamo:key|>lang<|plamo:val|>en",
		"<|plamo:key|>form<|plamo:val|>polite",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if fake.last.MaxTokens != 256 {
		t.Errorf("max tokens = %d", fake.last.MaxTokens)
	}
}

func TestTextQAliases(t *testing.T) {
	fake := &fakeBackend{}
	server := newAPI(t, fake)

	cases := []struct {
		name string
		body string
		code int
		err  string
	}{
		{"q only", `{"q":"Hello"}`, 200, ""},
		{"text only", `{"text":"Hello"}`, 200, ""},
		{"both equal", `{"text":"Hello","q":"Hello"}`, 200, ""},
		{"both different", `{"text":"Hello","q":"Bye"}`, 400, "ambiguous_text"},
		{"neither", `{}`, 400, "text_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postJSON(t, server.URL+"/translate", tc.body)
			if resp.StatusCode != tc.code {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.code, body)
			}
			if tc.err != "" {
				if got := decodeError(t, body).Error.Code; got != tc.err {
					t.Fatalf("code = %q, want %q", got, tc.err)
				}
			}
		})
	}
}

func TestTranslateValidationAndBodies(t *testing.T) {
	fake := &fakeBackend{}
	server := newAPI(t, fake)

	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"bad target", `{"text":"x","target":"xx"}`, 400, "invalid_target_language"},
		{"bad source", `{"text":"x","source":"xx"}`, 400, "invalid_source_language"},
		{"bad style", `{"text":"x","style":"casual"}`, 400, "invalid_style"},
		{"reserved", `{"text":"x<|plamo:tag|>"}`, 400, "reserved_token"},
		{"invalid json", `{"text":`, 400, "invalid_json"},
		{"trailing", `{"text":"x"} {}`, 400, "invalid_json"},
		{"max tokens", `{"text":"x","max_tokens":99999}`, 400, "invalid_max_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postJSON(t, server.URL+"/translate", tc.body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.status, body)
			}
			if got := decodeError(t, body).Error.Code; got != tc.code {
				t.Fatalf("code = %q, want %q", got, tc.code)
			}
		})
	}

	small := newAPI(t, fake, func(o *Options) { o.MaxBody = 64 })
	resp, body := postJSON(t, small.URL+"/translate", `{"text":"`+strings.Repeat("a", 200)+`"}`)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d (%s)", resp.StatusCode, body)
	}
	if got := decodeError(t, body).Error.Code; got != "payload_too_large" {
		t.Fatalf("code = %s", got)
	}
}

func TestTranslateUpstreamFailures(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{backend.ErrTruncated, "backend_truncated"},
		{backend.ErrEmpty, "backend_empty"},
		{backend.ErrMalformed, "backend_malformed"},
		{backend.ErrUnsafeOutput, "unsafe_output"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			server := newAPI(t, &fakeBackend{err: tc.err})
			resp, body := postJSON(t, server.URL+"/translate", `{"text":"Hello"}`)
			if resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("status = %d (%s)", resp.StatusCode, body)
			}
			if got := decodeError(t, body).Error.Code; got != tc.code {
				t.Fatalf("code = %s", got)
			}
		})
	}
}

func TestChatCompletionsTranslationOnly(t *testing.T) {
	fake := &fakeBackend{text: "こんにちは"}
	server := newAPI(t, fake)

	resp, body := postJSON(t, server.URL+"/v1/chat/completions", `{
		"model":"test-model",
		"messages":[{"role":"user","content":"Hello"}],
		"source_lang":"en",
		"target_lang":"ja",
		"temperature":0,
		"max_tokens":64
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d (%s)", resp.StatusCode, body)
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Object != "chat.completion" || len(decoded.Choices) != 1 {
		t.Fatalf("unexpected response: %+v", decoded)
	}
	if decoded.Choices[0].Message.Content != "こんにちは" || decoded.Choices[0].FinishReason != "stop" {
		t.Fatalf("choice = %+v", decoded.Choices[0])
	}
	if fake.last.MaxTokens != 64 {
		t.Fatalf("max tokens = %d", fake.last.MaxTokens)
	}
	if !strings.Contains(fake.last.Prompt, "<|plamo:key|>lang<|plamo:val|>en") {
		t.Fatalf("source lang missing: %s", fake.last.Prompt)
	}

	rejects := []struct {
		name string
		body string
		code string
	}{
		{"stream", `{"messages":[{"role":"user","content":"x"}],"stream":true}`, "stream_unsupported"},
		{"model", `{"model":"wrong","messages":[{"role":"user","content":"x"}]}`, "model_unsupported"},
		{"stop", `{"stop":["x"],"messages":[{"role":"user","content":"x"}]}`, "stop_unsupported"},
		{"system", `{"messages":[{"role":"system","content":"x"},{"role":"user","content":"y"}]}`, "unsupported_messages"},
		{"assistant only", `{"messages":[{"role":"assistant","content":"x"}]}`, "unsupported_messages"},
		{"history", `{"messages":[{"role":"user","content":"x"},{"role":"assistant","content":"y"},{"role":"user","content":"z"}]}`, "unsupported_messages"},
		{"temperature", `{"messages":[{"role":"user","content":"x"}],"temperature":0.7}`, "invalid_temperature"},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postJSON(t, server.URL+"/v1/chat/completions", tc.body)
			if resp.StatusCode != 400 {
				t.Fatalf("status = %d (%s)", resp.StatusCode, body)
			}
			if got := decodeError(t, body).Error.Code; got != tc.code {
				t.Fatalf("code = %s", got)
			}
		})
	}
}

func TestHealthReadyLanguages(t *testing.T) {
	fake := &fakeBackend{}
	server := newAPI(t, fake)

	resp, body := get(t, server.URL+"/health")
	if resp.StatusCode != 200 {
		t.Fatalf("health = %d (%s)", resp.StatusCode, body)
	}
	var health map[string]any
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" || health["model"] != "test-model" {
		t.Fatalf("health = %+v", health)
	}

	resp, body = get(t, server.URL+"/ready")
	if resp.StatusCode != 200 {
		t.Fatalf("ready = %d (%s)", resp.StatusCode, body)
	}

	fake.healthErr = errors.New("loading")
	resp, body = get(t, server.URL+"/ready")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ready = %d (%s)", resp.StatusCode, body)
	}
	if got := decodeError(t, body).Error.Code; got != "not_ready" {
		t.Fatalf("code = %s", got)
	}

	resp, body = get(t, server.URL+"/languages")
	if resp.StatusCode != 200 {
		t.Fatalf("languages = %d", resp.StatusCode)
	}
	var langs struct {
		Count     int `json:"count"`
		Languages []struct {
			Code string `json:"code"`
		} `json:"languages"`
	}
	if err := json.Unmarshal(body, &langs); err != nil {
		t.Fatal(err)
	}
	if langs.Count != 53 || len(langs.Languages) != 53 {
		t.Fatalf("count = %d len = %d", langs.Count, len(langs.Languages))
	}
}

func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func TestMethodsAndUnknownPaths(t *testing.T) {
	server := newAPI(t, &fakeBackend{})

	resp, _ := get(t, server.URL+"/translate")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /translate = %d", resp.StatusCode)
	}
	if resp.Header.Get("Allow") != "POST" {
		t.Fatalf("Allow = %s", resp.Header.Get("Allow"))
	}

	resp, _ = get(t, server.URL+"/v1/completions")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/v1/completions = %d", resp.StatusCode)
	}

	resp, body := get(t, server.URL+"/")
	if resp.StatusCode != 200 {
		t.Fatalf("root = %d (%s)", resp.StatusCode, body)
	}
}

func TestCORSIsExplicit(t *testing.T) {
	server := newAPI(t, &fakeBackend{}, func(o *Options) {
		o.CORSOrigins = []string{"https://example.test"}
	})

	req, _ := http.NewRequest(http.MethodOptions, server.URL+"/translate", nil)
	req.Header.Set("Origin", "https://example.test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("allow origin = %q", got)
	}

	req, _ = http.NewRequest(http.MethodOptions, server.URL+"/translate", nil)
	req.Header.Set("Origin", "https://denied.test")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied OPTIONS = %d", resp.StatusCode)
	}

	plain := newAPI(t, &fakeBackend{})
	req, _ = http.NewRequest(http.MethodPost, plain.URL+"/translate", bytes.NewReader([]byte(`{"text":"x"}`)))
	req.Header.Set("Origin", "https://example.test")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS header: %q", got)
	}
}
