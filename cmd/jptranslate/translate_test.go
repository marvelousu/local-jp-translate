package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr := fn()
	_ = w.Close()
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), callErr
}

func TestTranslateCommand(t *testing.T) {
	var got translateCallRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/translate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"translated_text": "こんにちは",
			"translatedText":  "こんにちは",
			"model":           "test-model",
			"duration_ms":     1,
		})
	}))
	defer server.Close()

	out, err := captureStdout(t, func() error {
		return runTranslate([]string{"Hello", "--url", server.URL, "--target", "ja", "--style", "polite", "--glossary", "Hello => こんにちは"})
	})
	if err != nil {
		t.Fatalf("runTranslate: %v", err)
	}
	if out != "こんにちは\n" {
		t.Fatalf("stdout = %q", out)
	}
	if got.Text != "Hello" || got.Target != "ja" || got.Style != "polite" {
		t.Fatalf("request = %+v", got)
	}
	if len(got.Glossary) != 1 || got.Glossary[0].Source != "Hello" {
		t.Fatalf("glossary = %+v", got.Glossary)
	}
}

func TestTranslateCommandAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_target_language","message":"bad target"}}`))
	}))
	defer server.Close()

	_, err := captureStdout(t, func() error {
		return runTranslate([]string{"Hello", "--url", server.URL})
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_target_language") {
		t.Fatalf("err = %v", err)
	}
}

func TestTranslateCommandRejectsAmbiguousInput(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return runTranslate([]string{"Hello", "--text", "Hello"})
	})
	if err == nil || !strings.Contains(err.Error(), "either positionally") {
		t.Fatalf("err = %v", err)
	}
}
