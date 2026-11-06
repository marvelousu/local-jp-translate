// Package httpapi exposes the local translation HTTP API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marvelousu/local-jp-translate/internal/plamo"
	"github.com/marvelousu/local-jp-translate/internal/translate"
)

// Options configures the HTTP server.
type Options struct {
	Service     *translate.Service
	Version     string
	MaxBody     int64
	CORSOrigins []string
	Logger      *log.Logger
	Now         func() time.Time
}

// Server is the API handler.
type Server struct {
	svc     *translate.Service
	version string
	maxBody int64
	cors    map[string]struct{}
	logger  *log.Logger
	started time.Time
	now     func() time.Time
	chatSeq atomic.Uint64
	handler http.Handler
}

// New creates a Server. MaxBody defaults to 1 MiB.
func New(opts Options) *Server {
	if opts.MaxBody <= 0 {
		opts.MaxBody = 1 << 20
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Server{
		svc:     opts.Service,
		version: opts.Version,
		maxBody: opts.MaxBody,
		cors:    make(map[string]struct{}),
		logger:  opts.Logger,
		started: opts.Now(),
		now:     opts.Now,
	}
	for _, origin := range opts.CORSOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			s.cors[origin] = struct{}{}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/translate", s.handleTranslate)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/languages", s.handleLanguages)
	mux.HandleFunc("/", s.handleRoot)

	s.handler = s.recoverPanic(s.corsMiddleware(s.accessLog(mux)))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.handler.ServeHTTP(w, r)
}

// Handler returns the composed handler.
func (s *Server) Handler() http.Handler { return s }

type translateRequest struct {
	Text      *string        `json:"text"`
	Q         *string        `json:"q"`
	Source    string         `json:"source"`
	Target    string         `json:"target"`
	Style     string         `json:"style"`
	Glossary  []glossaryItem `json:"glossary"`
	Context   string         `json:"context"`
	MaxTokens int            `json:"max_tokens"`
}

type glossaryItem struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type translateResponse struct {
	TranslatedText      string `json:"translated_text"`
	TranslatedTextCamel string `json:"translatedText"`
	Model               string `json:"model"`
	DurationMS          int64  `json:"duration_ms"`
}

func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "translation service is not configured")
		return
	}

	var req translateRequest
	if err := s.decode(w, r, &req); err != nil {
		return
	}

	text, apiErr := pickText(req)
	if apiErr != nil {
		writeError(w, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}

	glossary := make([]plamo.GlossaryEntry, len(req.Glossary))
	for i, entry := range req.Glossary {
		glossary[i] = plamo.GlossaryEntry{Source: entry.Source, Target: entry.Target}
	}

	result, err := s.svc.Translate(r.Context(), translate.Request{
		Text:      text,
		Source:    req.Source,
		Target:    req.Target,
		Style:     req.Style,
		Glossary:  glossary,
		Context:   req.Context,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, translateResponse{
		TranslatedText:      result.Text,
		TranslatedTextCamel: result.Text,
		Model:               result.Model,
		DurationMS:          result.DurationMS,
	})
}

func pickText(req translateRequest) (string, *translate.Error) {
	switch {
	case req.Text != nil && req.Q != nil:
		if *req.Text != *req.Q {
			return "", &translate.Error{
				Code:    "ambiguous_text",
				Message: "provide either text or q, not both with different values",
				Status:  http.StatusBadRequest,
			}
		}
		return *req.Text, nil
	case req.Text != nil:
		return *req.Text, nil
	case req.Q != nil:
		return *req.Q, nil
	default:
		return "", &translate.Error{
			Code:    "text_required",
			Message: "text or q is required",
			Status:  http.StatusBadRequest,
		}
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	TargetLang  string        `json:"target_lang"`
	SourceLang  string        `json:"source_lang"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature *float64      `json:"temperature"`
	Stop        []string      `json:"stop"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if s.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "translation service is not configured")
		return
	}

	var req chatRequest
	if err := s.decode(w, r, &req); err != nil {
		return
	}
	if req.Stream {
		writeError(w, http.StatusBadRequest, "stream_unsupported", "this translation-only endpoint supports nonstreaming requests only")
		return
	}
	if len(req.Stop) != 0 {
		writeError(w, http.StatusBadRequest, "stop_unsupported", "translation uses fixed model stop markers; custom stop is unsupported")
		return
	}
	if req.Model != "" && req.Model != s.svc.Model() {
		writeError(w, http.StatusBadRequest, "model_unsupported", "model must match the configured translation model")
		return
	}
	if req.Temperature != nil && *req.Temperature != 0 {
		writeError(w, http.StatusBadRequest, "invalid_temperature", "temperature must be 0; arbitrary sampling is not supported")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "unsupported_messages", "provide exactly one user message; system and assistant messages are not supported")
		return
	}
	if len(req.Messages) != 1 {
		writeError(w, http.StatusBadRequest, "unsupported_messages", "provide exactly one user message; conversation history is not supported by this compatibility endpoint")
		return
	}
	if req.Messages[0].Role != "user" {
		writeError(w, http.StatusBadRequest, "unsupported_messages", "the only message must have role user; system prompts are not supported")
		return
	}

	result, err := s.svc.Translate(r.Context(), translate.Request{
		Text:      req.Messages[0].Content,
		Source:    req.SourceLang,
		Target:    req.TargetLang,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		s.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d-%d", s.now().Unix(), s.chatSeq.Add(1)),
		Object:  "chat.completion",
		Created: s.now().Unix(),
		Model:   result.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatMessage{Role: "assistant", Content: result.Text},
			FinishReason: "stop",
		}},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	inflight, waiting := 0, 0
	model := ""
	if s.svc != nil {
		inflight, waiting = s.svc.Stats()
		model = s.svc.Model()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"service":        "local-jp-translate",
		"version":        s.version,
		"model":          model,
		"uptime_seconds": int64(s.now().Sub(s.started).Seconds()),
		"queue": map[string]int{
			"inflight": inflight,
			"waiting":  waiting,
		},
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if s.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "translation service is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.svc.Ready(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "llama-server is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": true,
		"model": s.svc.Model(),
	})
}

func (s *Server) handleLanguages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"languages":      plamo.SupportedLanguages,
		"count":          len(plamo.SupportedLanguages),
		"default_target": "ja",
		"source_auto":    "auto",
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not_found", "unknown endpoint")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "local-jp-translate",
		"version": s.version,
		"endpoints": []string{
			"POST /translate",
			"POST /v1/chat/completions",
			"GET /health",
			"GET /ready",
			"GET /languages",
		},
	})
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("request body exceeds %d bytes", s.maxBody))
			return err
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a single JSON object: "+err.Error())
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON object")
		return errors.New("trailing JSON data")
	}
	return nil
}

func (s *Server) writeServiceError(w http.ResponseWriter, err error) {
	var svcErr *translate.Error
	if errors.As(err, &svcErr) {
		if svcErr.Status == http.StatusTooManyRequests && w.Header().Get("Retry-After") == "" {
			w.Header().Set("Retry-After", "1")
		}
		writeError(w, svcErr.Status, svcErr.Code, svcErr.Message)
		return
	}
	if errors.Is(err, context.Canceled) {
		// The client went away; there is nobody to answer.
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "upstream_timeout", "translation request timed out")
		return
	}
	if s.logger != nil {
		s.logger.Printf("translation error: %v", err)
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := s.cors[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Max-Age", "600")
			} else if r.Method == http.MethodOptions {
				writeError(w, http.StatusForbidden, "cors_origin_denied", "origin is not allowed")
				return
			}
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				w.Header().Set("Allow", "GET, POST, OPTIONS")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.logger != nil {
					s.logger.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				}
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	if s.logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Printf("%s %s %s %d %s", r.RemoteAddr, r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed; use "+allowed)
}

// ParseRetryAfter is a small helper for clients reading Retry-After headers.
func ParseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
