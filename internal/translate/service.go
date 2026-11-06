// Package translate validates translation requests, builds PFML3 prompts, and
// serializes inference through one slot with a bounded waiting queue.
package translate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/marvelousu/local-jp-translate/internal/backend"
	"github.com/marvelousu/local-jp-translate/internal/plamo"
)

// Error is an API-safe error with a stable code and HTTP status.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func validationError(code, message string) *Error {
	return &Error{Code: code, Message: message, Status: http.StatusBadRequest}
}

// Request is a normalized translation request.
type Request struct {
	Text      string
	Source    string
	Target    string
	Style     string
	Glossary  []plamo.GlossaryEntry
	Context   string
	MaxTokens int
}

// Result is a successful translation.
type Result struct {
	Text             string
	Model            string
	DurationMS       int64
	CompletionTokens int
}

// Config controls validation and inference bounds.
type Config struct {
	Model                string
	DefaultMaxTokens     int
	MaxMaxTokens         int
	MaxTextBytes         int
	MaxContextBytes      int
	MaxGlossaryEntries   int
	MaxGlossaryTermBytes int
	MaxPromptBytes       int
	RequestTimeout       time.Duration
	QueueSize            int
}

func (c Config) withDefaults() Config {
	if c.Model == "" {
		c.Model = "plamo-3-translate-0.8b-Q4_K_M"
	}
	if c.DefaultMaxTokens <= 0 {
		c.DefaultMaxTokens = 512
	}
	if c.MaxMaxTokens <= 0 {
		c.MaxMaxTokens = 2048
	}
	if c.MaxTextBytes <= 0 {
		c.MaxTextBytes = 32 << 10
	}
	if c.MaxContextBytes <= 0 {
		c.MaxContextBytes = 32 << 10
	}
	if c.MaxGlossaryEntries <= 0 {
		c.MaxGlossaryEntries = 128
	}
	if c.MaxGlossaryTermBytes <= 0 {
		c.MaxGlossaryTermBytes = 512
	}
	if c.MaxPromptBytes <= 0 {
		c.MaxPromptBytes = 256 << 10
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 180 * time.Second
	}
	if c.QueueSize < 0 {
		c.QueueSize = 0
	}
	return c
}

// Backend is the inference dependency.
type Backend interface {
	Complete(ctx context.Context, req backend.CompletionRequest) (backend.CompletionResult, error)
	Health(ctx context.Context) error
}

// Service owns the single inference slot and waiting queue.
type Service struct {
	cfg     Config
	backend Backend

	slot     chan struct{}
	queueMu  sync.Mutex
	waiting  int
	inflight int

	started time.Time
}

// NewService creates a service. A nil backend is allowed for tests that only
// exercise validation, but inference will fail.
func NewService(cfg Config, b Backend) *Service {
	return &Service{
		cfg:     cfg.withDefaults(),
		backend: b,
		slot:    make(chan struct{}, 1),
		started: time.Now(),
	}
}

// Config returns the effective configuration.
func (s *Service) Config() Config { return s.cfg }

// Model returns the model name reported to clients.
func (s *Service) Model() string { return s.cfg.Model }

// Stats reports active and queued request counts.
func (s *Service) Stats() (inflight, waiting int) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return s.inflight, s.waiting
}

// Ready checks the backend health endpoint.
func (s *Service) Ready(ctx context.Context) error {
	if s.backend == nil {
		return errors.New("backend is not configured")
	}
	return s.backend.Health(ctx)
}

// Translate validates, prompts, queues, and performs one translation.
func (s *Service) Translate(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	normalized, err := s.validate(req)
	if err != nil {
		return Result{}, err
	}

	prompt, err := plamo.BuildPrompt(plamo.PromptOptions{
		Text:       normalized.Text,
		SourceLang: normalized.Source,
		TargetLang: normalized.Target,
		Form:       normalized.Style,
		Context:    normalized.Context,
		Glossary:   normalized.Glossary,
	})
	if err != nil {
		var promptErr *plamo.PromptError
		if errors.As(err, &promptErr) {
			return Result{}, validationError(promptErr.Code, promptErr.Message)
		}
		return Result{}, validationError("invalid_request", err.Error())
	}
	if len(prompt) > s.cfg.MaxPromptBytes {
		return Result{}, validationError("text_too_long", fmt.Sprintf("prompt exceeds %d bytes", s.cfg.MaxPromptBytes))
	}

	requestCtx := ctx
	var cancel context.CancelFunc
	if s.cfg.RequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
		defer cancel()
	}

	if err := s.acquire(requestCtx); err != nil {
		return Result{}, s.queueError(ctx, requestCtx, err)
	}
	defer s.release()

	if s.backend == nil {
		return Result{}, &Error{Code: "backend_not_configured", Message: "inference backend is not configured", Status: http.StatusServiceUnavailable}
	}

	completion, err := s.backend.Complete(requestCtx, backend.CompletionRequest{
		Prompt:    prompt,
		MaxTokens: normalized.MaxTokens,
		Model:     s.cfg.Model,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		if requestCtx.Err() != nil {
			return Result{}, &Error{Code: "upstream_timeout", Message: "translation timed out waiting for the model", Status: http.StatusGatewayTimeout}
		}
		return Result{}, mapBackendError(err)
	}

	return Result{
		Text:             completion.Text,
		Model:            s.cfg.Model,
		DurationMS:       time.Since(start).Milliseconds(),
		CompletionTokens: completion.CompletionTokens,
	}, nil
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.slot <- struct{}{}:
		s.queueMu.Lock()
		s.inflight++
		s.queueMu.Unlock()
		return nil
	default:
	}

	s.queueMu.Lock()
	if s.waiting >= s.cfg.QueueSize {
		s.queueMu.Unlock()
		return errQueueFull
	}
	s.waiting++
	s.queueMu.Unlock()

	defer func() {
		s.queueMu.Lock()
		s.waiting--
		s.queueMu.Unlock()
	}()

	select {
	case s.slot <- struct{}{}:
		s.queueMu.Lock()
		s.inflight++
		s.queueMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() {
	<-s.slot
	s.queueMu.Lock()
	s.inflight--
	s.queueMu.Unlock()
}

var errQueueFull = errors.New("queue is full")

func (s *Service) queueError(parent, requestCtx context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(err, errQueueFull) {
		return &Error{
			Code:    "overloaded",
			Message: "the inference queue is full; retry later",
			Status:  http.StatusTooManyRequests,
		}
	}
	if requestCtx.Err() != nil {
		return &Error{Code: "request_timeout", Message: "timed out waiting for the inference slot", Status: http.StatusGatewayTimeout}
	}
	return &Error{Code: "overloaded", Message: "could not acquire the inference slot", Status: http.StatusServiceUnavailable}
}

func (s *Service) validate(req Request) (Request, error) {
	if strings.TrimSpace(req.Text) == "" {
		return Request{}, validationError("text_required", "text must not be empty")
	}
	if !utf8.ValidString(req.Text) {
		return Request{}, validationError("invalid_text", "text must be valid UTF-8")
	}
	if len(req.Text) > s.cfg.MaxTextBytes {
		return Request{}, validationError("text_too_long", fmt.Sprintf("text exceeds %d bytes", s.cfg.MaxTextBytes))
	}

	target := strings.TrimSpace(req.Target)
	if target == "" {
		target = "ja"
	}
	if !plamo.IsSupported(target) {
		return Request{}, validationError("invalid_target_language", "unsupported target language: "+target)
	}

	source := strings.TrimSpace(req.Source)
	if source == "auto" {
		source = ""
	}
	if source != "" && !plamo.IsSupported(source) {
		return Request{}, validationError("invalid_source_language", `unsupported source language: `+source+` (use "auto" or a supported code)`)
	}

	style := strings.TrimSpace(req.Style)
	if style == "default" {
		style = ""
	}
	if style != "" && style != "polite" && style != "plain" {
		return Request{}, validationError("invalid_style", "style must be polite, plain, or default")
	}
	if style != "" && target != "ja" {
		return Request{}, validationError("style_target_mismatch", "style is only supported when target is ja")
	}

	if len(req.Context) > s.cfg.MaxContextBytes {
		return Request{}, validationError("context_too_long", fmt.Sprintf("context exceeds %d bytes", s.cfg.MaxContextBytes))
	}
	if !utf8.ValidString(req.Context) {
		return Request{}, validationError("invalid_context", "context must be valid UTF-8")
	}
	if len(req.Glossary) > s.cfg.MaxGlossaryEntries {
		return Request{}, validationError("invalid_glossary", fmt.Sprintf("glossary exceeds %d entries", s.cfg.MaxGlossaryEntries))
	}
	glossary := make([]plamo.GlossaryEntry, len(req.Glossary))
	for i, entry := range req.Glossary {
		if strings.TrimSpace(entry.Source) == "" || strings.TrimSpace(entry.Target) == "" {
			return Request{}, validationError("invalid_glossary", fmt.Sprintf("glossary entry %d needs non-empty source and target", i))
		}
		if len(entry.Source) > s.cfg.MaxGlossaryTermBytes || len(entry.Target) > s.cfg.MaxGlossaryTermBytes {
			return Request{}, validationError("invalid_glossary", fmt.Sprintf("glossary entry %d exceeds %d bytes", i, s.cfg.MaxGlossaryTermBytes))
		}
		if !utf8.ValidString(entry.Source) || !utf8.ValidString(entry.Target) {
			return Request{}, validationError("invalid_glossary", fmt.Sprintf("glossary entry %d must be valid UTF-8", i))
		}
		glossary[i] = entry
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = s.cfg.DefaultMaxTokens
	}
	if maxTokens < 1 || maxTokens > s.cfg.MaxMaxTokens {
		return Request{}, validationError("invalid_max_tokens", fmt.Sprintf("max_tokens must be between 1 and %d", s.cfg.MaxMaxTokens))
	}

	return Request{
		Text:      req.Text,
		Source:    source,
		Target:    target,
		Style:     style,
		Glossary:  glossary,
		Context:   req.Context,
		MaxTokens: maxTokens,
	}, nil
}

func mapBackendError(err error) *Error {
	switch {
	case errors.Is(err, backend.ErrTruncated):
		return &Error{Code: "backend_truncated", Message: "model output hit max_tokens and was rejected as incomplete; retry with a larger max_tokens", Status: http.StatusBadGateway}
	case errors.Is(err, backend.ErrEmpty):
		return &Error{Code: "backend_empty", Message: "model returned an empty translation", Status: http.StatusBadGateway}
	case errors.Is(err, backend.ErrMalformed):
		return &Error{Code: "backend_malformed", Message: "model backend returned a malformed response", Status: http.StatusBadGateway}
	case errors.Is(err, backend.ErrIncomplete):
		return &Error{Code: "backend_incomplete", Message: "model did not finish the translation normally", Status: http.StatusBadGateway}
	case errors.Is(err, backend.ErrUnsafeOutput):
		return &Error{Code: "unsafe_output", Message: "model output contained a reserved PFML marker and was rejected", Status: http.StatusBadGateway}
	}
	var httpErr *backend.HTTPError
	if errors.As(err, &httpErr) {
		return &Error{Code: "backend_error", Message: fmt.Sprintf("model backend returned HTTP %d", httpErr.StatusCode), Status: http.StatusBadGateway}
	}
	return &Error{Code: "backend_error", Message: "model backend request failed", Status: http.StatusBadGateway}
}
