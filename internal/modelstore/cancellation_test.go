package modelstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelDuringBodyRemovesFilesAndAllowsRetry(t *testing.T) {
	data := []byte("fixture weights")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if calls.Add(1) == 1 {
			w.Write(data[:1])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	go func() {
		_, err := install(ctx, dir, server.URL, "model.gguf", int64(len(data)), digest, server.Client())
		finished <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waiting := true
	for waiting {
		select {
		case <-ticker.C:
			partials, _ := filepath.Glob(filepath.Join(dir, ".model-*.partial"))
			waiting = len(partials) == 0
		case <-deadline.C:
			cancel()
			t.Fatal("download did not create partial file")
		}
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("download cancellation: %v", err)
		}
	case <-deadline.C:
		t.Fatal("download did not stop")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancellation left files: %v, %v", entries, err)
	}
	if _, err := install(context.Background(), dir, server.URL, "model.gguf", int64(len(data)), digest, server.Client()); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestDownloadUsesEnvironmentProxy(t *testing.T) {
	if os.Getenv("JPTRANSLATE_TEST_PROXY") == "1" {
		req, _ := http.NewRequest(http.MethodGet, "https://huggingface.co/model", nil)
		proxy, err := downloadTransport().Proxy(req)
		if err != nil || proxy == nil || proxy.String() != "http://127.0.0.1:45678" {
			t.Fatalf("environment proxy = %v, %v", proxy, err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDownloadUsesEnvironmentProxy$")
	for _, entry := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if key != "HTTPS_PROXY" && key != "HTTP_PROXY" && key != "NO_PROXY" && key != "REQUEST_METHOD" && key != "JPTRANSLATE_TEST_PROXY" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "JPTRANSLATE_TEST_PROXY=1", "HTTPS_PROXY=http://127.0.0.1:45678")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("proxy subprocess: %v\n%s", err, output)
	}
}
