package backend

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestBackendDoesNotFollowRedirect(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := NewClient(origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), CompletionRequest{Prompt: "private text", MaxTokens: 32}); err == nil {
		t.Fatal("accepted redirect")
	}
	if called {
		t.Fatal("translation forwarded to redirect destination")
	}
}

func TestManagedRejectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if managed, err := StartManaged(context.Background(), ManagedConfig{Executable: exe, Model: path, Port: port}); err == nil {
		managed.Stop(context.Background())
		t.Fatal("accepted occupied backend port")
	}
}
