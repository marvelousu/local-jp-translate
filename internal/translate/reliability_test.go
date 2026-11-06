package translate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/marvelousu/local-jp-translate/internal/backend"
)

func TestLanguageSpellingsProduceCanonicalPrompt(t *testing.T) {
	for _, tc := range []struct{ source, target, canonical string }{
		{"EN", "JA", "ja"}, {"en", "zh-Hans", "zh-hans"},
		{"en", "zh_CN", "zh-hans"}, {"AUTO", "zh-TW", "zh-hant"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			fake := &fakeBackend{}
			svc := NewService(Config{}, fake)
			_, err := svc.Translate(context.Background(), Request{Text: "Hello", Source: tc.source, Target: tc.target})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(fake.last.Prompt, tc.canonical) {
				t.Fatalf("missing canonical code %s in prompt %s", tc.canonical, fake.last.Prompt)
			}
		})
	}
	fake := &fakeBackend{}
	_, err := NewService(Config{}, fake).Translate(context.Background(), Request{Text: "Hello", Target: "pt-BR"})
	var serviceErr *Error
	if !errors.As(err, &serviceErr) || serviceErr.Code != "invalid_target_language" || fake.calls != 0 {
		t.Fatalf("unsupported language reached backend: calls=%d error=%v", fake.calls, err)
	}
}

func TestContextOverflowExplainsRemediesWithoutBackendBody(t *testing.T) {
	fake := &fakeBackend{complete: func(context.Context, backend.CompletionRequest) (backend.CompletionResult, error) {
		return backend.CompletionResult{}, &backend.HTTPError{StatusCode: 400, Body: "request exceeds available context size: PRIVATE_INPUT"}
	}}
	_, err := NewService(Config{}, fake).Translate(context.Background(), Request{Text: "Hello", Target: "ja"})
	var serviceErr *Error
	if !errors.As(err, &serviceErr) || serviceErr.Status != http.StatusRequestEntityTooLarge || serviceErr.Code != "context_window_exceeded" {
		t.Fatalf("context error: %v", err)
	}
	if !strings.Contains(serviceErr.Message, "--llama-context") || strings.Contains(serviceErr.Message, "PRIVATE_INPUT") {
		t.Fatalf("unsafe or unhelpful message: %s", serviceErr.Message)
	}
}
