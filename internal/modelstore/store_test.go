package modelstore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedInstallAndReuse(t *testing.T) {
	data := []byte("fixture weights")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write(data) }))
	defer s.Close()
	dir := t.TempDir()
	for range 2 {
		path, err := install(context.Background(), dir, s.URL, "model.gguf", int64(len(data)), digest, s.Client())
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(data) {
			t.Fatal("model data differs")
		}
	}
	if calls != 1 {
		t.Fatalf("downloaded %d times", calls)
	}
}

func TestBadDownloadNeverReplacesExistingModel(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("corrupt")) }))
	defer s.Close()
	dir := t.TempDir()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("correct")))
	if _, err := install(context.Background(), dir, s.URL, "model.gguf", 7, digest, s.Client()); err == nil {
		t.Fatal("accepted corrupt download")
	}
	if _, err := os.Stat(filepath.Join(dir, "model.gguf")); !os.IsNotExist(err) {
		t.Fatal("corrupt download was installed")
	}
	if err := os.WriteFile(filepath.Join(dir, "model.gguf"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), dir, s.URL, "model.gguf", 7, digest, s.Client()); err == nil {
		t.Fatal("accepted mismatched existing model")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "model.gguf"))
	if string(got) != "original" {
		t.Fatal("existing model overwritten")
	}
	partials, _ := filepath.Glob(filepath.Join(dir, "*.partial"))
	if len(partials) != 0 {
		t.Fatal("partial download not cleaned")
	}
}

func TestCancellationAndInstallLock(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := install(ctx, dir, "http://127.0.0.1:1", "model.gguf", 1, "bad", http.DefaultClient); err == nil {
		t.Fatal("canceled download accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "model.gguf.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := install(context.Background(), dir, "http://127.0.0.1:1", "model.gguf", 1, "bad", http.DefaultClient); err == nil {
		t.Fatal("concurrent install accepted")
	}
}
