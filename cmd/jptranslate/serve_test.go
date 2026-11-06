package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServeWiringWithExternalBackend(t *testing.T) {
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/completions":
			_, _ = w.Write([]byte(`{"choices":[{"text":"こんにちは","finish_reason":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer backendServer.Close()

	apiPort := freeTCPPort(t)
	addr := "127.0.0.1:" + strconv.Itoa(apiPort)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = serveWithContext(ctx, []string{
			"--backend", backendServer.URL,
			"--addr", addr,
			"--queue-size", "0",
			"--request-timeout", "10s",
			"--model-name", "test-model",
		})
		close(stopped)
	}()
	stop := func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("serve did not stop")
			return
		}
		if serveErr != nil {
			t.Errorf("serve returned: %v", serveErr)
		}
	}
	defer stop()

	baseURL := "http://" + addr
	waitFor(t, baseURL+"/health")

	body := `{"text":"Hello","source":"en","target":"ja"}`
	resp, err := http.Post(baseURL+"/translate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d (%s)", resp.StatusCode, data)
	}
	var decoded struct {
		TranslatedText string `json:"translated_text"`
		Model          string `json:"model"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TranslatedText != "こんにちは" || decoded.Model != "test-model" {
		t.Fatalf("response = %+v", decoded)
	}

	ready, err := http.Get(baseURL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	ready.Body.Close()
	if ready.StatusCode != http.StatusOK {
		t.Fatalf("ready = %d", ready.StatusCode)
	}

	stop()
}

func waitFor(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready at %s: %v", url, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
