package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestManagedHelperProcess is not a normal test: when the environment asks for
// it, this process becomes a tiny fake llama-server for lifecycle tests.
func TestManagedHelperProcess(t *testing.T) {
	if os.Getenv("JPTRANSLATE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	if os.Getenv("JPTRANSLATE_HELPER_FAIL") == "1" {
		os.Exit(3)
	}
	port := os.Getenv("JPTRANSLATE_HELPER_PORT")
	if port == "" {
		fmt.Fprintln(os.Stderr, "missing helper port")
		os.Exit(4)
	}
	loading := os.Getenv("JPTRANSLATE_HELPER_LOADING") == "1"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if loading {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/completions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"text":"ok","finish_reason":"stop"}]}`))
	})
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(5)
	}
}

func TestManagedLifecycle(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("not a real model"), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)

	managed, err := StartManaged(context.Background(), ManagedConfig{
		Executable:     exe,
		Model:          modelPath,
		Host:           "127.0.0.1",
		Port:           port,
		StartupTimeout: 10 * time.Second,
		Args:           []string{"-test.run=TestManagedHelperProcess"},
		Env: []string{
			"JPTRANSLATE_HELPER=1",
			"JPTRANSLATE_HELPER_PORT=" + strconv.Itoa(port),
		},
	})
	if err != nil {
		t.Fatalf("StartManaged: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := managed.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	if managed.BaseURL() != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("base URL = %s", managed.BaseURL())
	}
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(managed.BaseURL() + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := managed.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-managed.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit")
	}
	if _, err := client.Get(managed.BaseURL() + "/health"); err == nil {
		t.Fatal("helper server still responds after Stop")
	}
}

func TestManagedStartReportsEarlyExit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = StartManaged(context.Background(), ManagedConfig{
		Executable:     exe,
		Model:          modelPath,
		Host:           "127.0.0.1",
		Port:           freePort(t),
		StartupTimeout: 3 * time.Second,
		Args:           []string{"-test.run=TestManagedHelperProcess"},
		Env:            []string{"JPTRANSLATE_HELPER=1", "JPTRANSLATE_HELPER_FAIL=1"},
	})
	if err == nil {
		t.Fatal("expected startup failure")
	}
	if !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("early exit was not detected promptly")
	}
}

func TestManagedStartupTimeout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	_, err = StartManaged(context.Background(), ManagedConfig{
		Executable:     exe,
		Model:          modelPath,
		Host:           "127.0.0.1",
		Port:           port,
		StartupTimeout: 800 * time.Millisecond,
		Args:           []string{"-test.run=TestManagedHelperProcess"},
		Env: []string{
			"JPTRANSLATE_HELPER=1",
			"JPTRANSLATE_HELPER_LOADING=1",
			"JPTRANSLATE_HELPER_PORT=" + strconv.Itoa(port),
		},
	})
	if err == nil {
		t.Fatal("expected readiness timeout")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagedConfigValidation(t *testing.T) {
	modelPath := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(modelPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []ManagedConfig{
		{Executable: "", Model: modelPath},
		{Executable: "llama-server", Model: ""},
		{Executable: "llama-server", Model: filepath.Join(t.TempDir(), "missing.gguf")},
		{Executable: "llama-server", Model: modelPath, Host: "0.0.0.0"},
		{Executable: "llama-server", Model: modelPath, Port: 70000},
	}
	for i, cfg := range cases {
		if _, err := StartManaged(context.Background(), cfg); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
