package translate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/marvelousu/local-jp-translate/internal/backend"
	"github.com/marvelousu/local-jp-translate/internal/plamo"
)

type fakeBackend struct {
	complete func(context.Context, backend.CompletionRequest) (backend.CompletionResult, error)
	health   func(context.Context) error
	calls    int
	last     backend.CompletionRequest
}

func (f *fakeBackend) Complete(ctx context.Context, req backend.CompletionRequest) (backend.CompletionResult, error) {
	f.calls++
	f.last = req
	if f.complete != nil {
		return f.complete(ctx, req)
	}
	return backend.CompletionResult{Text: "訳文"}, nil
}

func (f *fakeBackend) Health(ctx context.Context) error {
	if f.health != nil {
		return f.health(ctx)
	}
	return nil
}

func TestTranslateSuccessAndNormalization(t *testing.T) {
	fake := &fakeBackend{}
	svc := NewService(Config{Model: "test-model"}, fake)
	result, err := svc.Translate(context.Background(), Request{
		Text:      "Hello.",
		Source:    "auto",
		Target:    "",
		Style:     "default",
		Glossary:  []plamo.GlossaryEntry{{Source: "hello", Target: "こんにちは"}},
		Context:   "Greeting.",
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if result.Text != "訳文" || result.Model != "test-model" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DurationMS < 0 {
		t.Fatal("duration must not be negative")
	}
	if fake.calls != 1 {
		t.Fatalf("backend calls = %d", fake.calls)
	}
	prompt := fake.last.Prompt
	if !strings.Contains(prompt, "<|plamo:tag|>output<|plamo:key|>lang<|plamo:val|>ja<|plamo:msg|>") {
		t.Fatalf("default target / auto source prompt wrong:\n%s", prompt)
	}
	if strings.Contains(prompt, "<|plamo:key|>lang<|plamo:val|>auto") {
		t.Fatal("auto source must be omitted")
	}
	if !strings.Contains(prompt, "<|plamo:tag|>context<|plamo:msg|>Greeting.") {
		t.Fatal("context missing")
	}
	if !strings.Contains(prompt, "<|plamo:index|>hello<|plamo:item|>こんにちは") {
		t.Fatal("glossary missing")
	}
	if fake.last.MaxTokens != 100 {
		t.Fatalf("max tokens = %d", fake.last.MaxTokens)
	}
}

func TestValidationErrors(t *testing.T) {
	fake := &fakeBackend{complete: func(context.Context, backend.CompletionRequest) (backend.CompletionResult, error) {
		t.Fatal("backend must not be called for invalid requests")
		return backend.CompletionResult{}, nil
	}}
	svc := NewService(Config{
		MaxTextBytes:         100,
		MaxContextBytes:      10,
		MaxGlossaryEntries:   1,
		MaxGlossaryTermBytes: 5,
		MaxMaxTokens:         100,
		DefaultMaxTokens:     50,
	}, fake)

	cases := []struct {
		name   string
		req    Request
		code   string
		status int
	}{
		{"empty", Request{Text: "  "}, "text_required", 400},
		{"long text", Request{Text: strings.Repeat("a", 101)}, "text_too_long", 400},
		{"bad target", Request{Text: "x", Target: "xx"}, "invalid_target_language", 400},
		{"bad source", Request{Text: "x", Source: "xx"}, "invalid_source_language", 400},
		{"bad style", Request{Text: "x", Style: "casual"}, "invalid_style", 400},
		{"style with en", Request{Text: "x", Target: "en", Style: "polite"}, "style_target_mismatch", 400},
		{"long context", Request{Text: "x", Context: strings.Repeat("a", 11)}, "context_too_long", 400},
		{"too many glossary", Request{Text: "x", Glossary: []plamo.GlossaryEntry{{Source: "a", Target: "b"}, {Source: "c", Target: "d"}}}, "invalid_glossary", 400},
		{"empty glossary", Request{Text: "x", Glossary: []plamo.GlossaryEntry{{Source: "", Target: "b"}}}, "invalid_glossary", 400},
		{"long glossary", Request{Text: "x", Glossary: []plamo.GlossaryEntry{{Source: "abcdef", Target: "b"}}}, "invalid_glossary", 400},
		{"reserved", Request{Text: "x<|plamo:msg|>y"}, "reserved_token", 400},
		{"max tokens high", Request{Text: "x", MaxTokens: 101}, "invalid_max_tokens", 400},
		{"max tokens negative", Request{Text: "x", MaxTokens: -1}, "invalid_max_tokens", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Translate(context.Background(), tc.req)
			var svcErr *Error
			if !errors.As(err, &svcErr) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if svcErr.Code != tc.code || svcErr.Status != tc.status {
				t.Fatalf("got %s/%d, want %s/%d", svcErr.Code, svcErr.Status, tc.code, tc.status)
			}
		})
	}
	if fake.calls != 0 {
		t.Fatalf("backend calls = %d", fake.calls)
	}
}

func TestDefaultMaxTokens(t *testing.T) {
	fake := &fakeBackend{}
	svc := NewService(Config{DefaultMaxTokens: 77}, fake)
	if _, err := svc.Translate(context.Background(), Request{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if fake.last.MaxTokens != 77 {
		t.Fatalf("max tokens = %d", fake.last.MaxTokens)
	}
}

func TestBackendErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		code   string
		status int
	}{
		{backend.ErrTruncated, "backend_truncated", 502},
		{backend.ErrEmpty, "backend_empty", 502},
		{backend.ErrMalformed, "backend_malformed", 502},
		{backend.ErrIncomplete, "backend_incomplete", 502},
		{backend.ErrUnsafeOutput, "unsafe_output", 502},
		{&backend.HTTPError{StatusCode: 500}, "backend_error", 502},
		{errors.New("transport boom"), "backend_error", 502},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			fake := &fakeBackend{complete: func(context.Context, backend.CompletionRequest) (backend.CompletionResult, error) {
				return backend.CompletionResult{}, tc.err
			}}
			svc := NewService(Config{}, fake)
			_, err := svc.Translate(context.Background(), Request{Text: "x"})
			var svcErr *Error
			if !errors.As(err, &svcErr) {
				t.Fatalf("err = %v", err)
			}
			if svcErr.Code != tc.code || svcErr.Status != tc.status {
				t.Fatalf("got %s/%d, want %s/%d", svcErr.Code, svcErr.Status, tc.code, tc.status)
			}
		})
	}
}

func TestRequestTimeout(t *testing.T) {
	started := make(chan struct{})
	fake := &fakeBackend{complete: func(ctx context.Context, _ backend.CompletionRequest) (backend.CompletionResult, error) {
		close(started)
		<-ctx.Done()
		return backend.CompletionResult{}, ctx.Err()
	}}
	svc := NewService(Config{RequestTimeout: 50 * time.Millisecond}, fake)
	_, err := svc.Translate(context.Background(), Request{Text: "x"})
	select {
	case <-started:
	default:
		t.Fatal("backend was not called")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) {
		t.Fatalf("err = %v", err)
	}
	if svcErr.Code != "upstream_timeout" || svcErr.Status != http.StatusGatewayTimeout {
		t.Fatalf("got %s/%d", svcErr.Code, svcErr.Status)
	}
}

func TestClientCancellationStopsUpstream(t *testing.T) {
	started := make(chan struct{})
	upstreamDone := make(chan struct{})
	fake := &fakeBackend{complete: func(ctx context.Context, _ backend.CompletionRequest) (backend.CompletionResult, error) {
		close(started)
		<-ctx.Done()
		close(upstreamDone)
		return backend.CompletionResult{}, ctx.Err()
	}}
	svc := NewService(Config{RequestTimeout: 10 * time.Second}, fake)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Translate(ctx, Request{Text: "x"})
		errCh <- err
	}()
	<-started
	cancel()
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("upstream context was not canceled")
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Translate did not return")
	}
}

func TestOverloadAndQueue(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var calls int
	fake := &fakeBackend{complete: func(ctx context.Context, _ backend.CompletionRequest) (backend.CompletionResult, error) {
		calls++
		if calls == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return backend.CompletionResult{}, ctx.Err()
			}
		}
		return backend.CompletionResult{Text: "ok"}, nil
	}}
	svc := NewService(Config{QueueSize: 1, RequestTimeout: 5 * time.Second}, fake)

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.Translate(context.Background(), Request{Text: "one"})
		firstDone <- err
	}()
	<-started

	secondDone := make(chan error, 1)
	go func() {
		_, err := svc.Translate(context.Background(), Request{Text: "two"})
		secondDone <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, waiting := svc.Stats()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second request did not queue; stats waiting=%d", waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, err := svc.Translate(context.Background(), Request{Text: "three"})
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != "overloaded" || svcErr.Status != http.StatusTooManyRequests {
		t.Fatalf("third request = %v, want 429 overloaded", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second: %v", err)
	}
	inflight, waiting := svc.Stats()
	if inflight != 0 || waiting != 0 {
		t.Fatalf("stats not drained: inflight=%d waiting=%d", inflight, waiting)
	}
}

func TestQueuedRequestCancellation(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var calls int
	fake := &fakeBackend{complete: func(ctx context.Context, _ backend.CompletionRequest) (backend.CompletionResult, error) {
		calls++
		if calls == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return backend.CompletionResult{Text: "ok"}, nil
	}}
	svc := NewService(Config{QueueSize: 1, RequestTimeout: 5 * time.Second}, fake)
	go func() { _, _ = svc.Translate(context.Background(), Request{Text: "one"}) }()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.Translate(ctx, Request{Text: "two"})
		errCh <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		_, waiting := svc.Stats()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request did not queue")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request did not cancel")
	}
	close(release)
	deadline = time.Now().Add(time.Second)
	for {
		inflight, waiting := svc.Stats()
		if inflight == 0 && waiting == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats not drained: inflight=%d waiting=%d", inflight, waiting)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReady(t *testing.T) {
	fake := &fakeBackend{health: func(context.Context) error { return errors.New("down") }}
	svc := NewService(Config{}, fake)
	if err := svc.Ready(context.Background()); err == nil {
		t.Fatal("expected readiness failure")
	}
	nilSvc := NewService(Config{}, nil)
	if err := nilSvc.Ready(context.Background()); err == nil {
		t.Fatal("expected failure without backend")
	}
}
