package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ManagedConfig describes a llama-server process owned by this program.
type ManagedConfig struct {
	Executable     string
	Model          string
	Host           string
	Port           int
	ContextSize    int
	Alias          string
	ExtraArgs      []string
	Args           []string
	Env            []string
	StartupTimeout time.Duration
	LogWriter      io.Writer
}

// Managed is a started llama-server child process.
type Managed struct {
	cmd     *exec.Cmd
	baseURL string
	logs    *logRing

	done    chan struct{}
	waitMu  sync.Mutex
	waitErr error

	stopOnce sync.Once
	stopErr  error
}

// StartManaged launches llama-server and waits until /health reports ready or
// the startup timeout elapses. The model file must already exist. On failure
// the child process is terminated before the error is returned.
func StartManaged(ctx context.Context, cfg ManagedConfig) (*Managed, error) {
	if strings.TrimSpace(cfg.Executable) == "" {
		return nil, errors.New("llama-server executable is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("model path is required")
	}
	info, err := os.Stat(cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("model path is a directory: %s", cfg.Model)
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if err := ValidateLoopbackHost(cfg.Host); err != nil {
		return nil, fmt.Errorf("llama-server host: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8788
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("llama-server port out of range: %d", cfg.Port)
	}
	if cfg.ContextSize == 0 {
		cfg.ContextSize = 16384
	}
	if cfg.ContextSize < 256 || cfg.ContextSize > 32768 {
		return nil, fmt.Errorf("llama-server context size must be 256..32768: %d", cfg.ContextSize)
	}
	if cfg.Alias == "" {
		cfg.Alias = "plamo-3-translate"
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 180 * time.Second
	}

	executable, err := resolveExecutable(cfg.Executable)
	if err != nil {
		return nil, err
	}

	baseURL := "http://" + hostPort(cfg.Host, cfg.Port)
	probe, err := net.Listen("tcp", hostPort(cfg.Host, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("managed backend port is unavailable: %w", err)
	}
	probe.Close()
	args := []string{
		"-m", cfg.Model,
		"-c", strconv.Itoa(cfg.ContextSize),
		"--host", cfg.Host,
		"--port", strconv.Itoa(cfg.Port),
		"--alias", cfg.Alias,
		"--jinja",
		"--no-prefill-assistant",
		"--no-context-shift",
	}
	args = append(args, cfg.ExtraArgs...)
	for _, arg := range cfg.ExtraArgs {
		flag := strings.SplitN(arg, "=", 2)[0]
		switch flag {
		case "--host", "--port", "--model", "-m", "--ctx-size", "-c", "--alias", "--context-shift":
			return nil, fmt.Errorf("managed flag %s cannot be overridden with --llama-arg", flag)
		}
	}
	if len(cfg.Args) > 0 {
		args = append([]string(nil), cfg.Args...)
	}

	logs := newLogRing(64 << 10)
	writers := []io.Writer{logs}
	if cfg.LogWriter != nil {
		writers = append(writers, cfg.LogWriter)
	}
	logOut := io.MultiWriter(writers...)

	cmd := exec.Command(executable, args...)
	cmd.Stdout = logOut
	cmd.Stderr = logOut
	cmd.Stdin = nil
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start llama-server: %w", err)
	}

	m := &Managed{
		cmd:     cmd,
		baseURL: baseURL,
		logs:    logs,
		done:    make(chan struct{}),
	}
	go func() {
		waitErr := cmd.Wait()
		m.waitMu.Lock()
		m.waitErr = waitErr
		m.waitMu.Unlock()
		close(m.done)
	}()

	readyCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	if err := m.waitReady(readyCtx, cfg.StartupTimeout); err != nil {
		_ = m.forceStop()
		return nil, err
	}
	return m, nil
}

// BaseURL is the loopback URL llama-server is listening on.
func (m *Managed) BaseURL() string { return m.baseURL }

// Tail returns the last captured stdout/stderr text.
func (m *Managed) Tail() string { return m.logs.String() }

// WaitError reports the process exit error after Done is closed.
func (m *Managed) WaitError() error {
	m.waitMu.Lock()
	defer m.waitMu.Unlock()
	return m.waitErr
}

// Done closes when the child process exits.
func (m *Managed) Done() <-chan struct{} { return m.done }

// Stop asks llama-server to exit, then terminates it if it does not stop
// promptly. It is safe to call more than once.
func (m *Managed) Stop(ctx context.Context) error {
	m.stopOnce.Do(func() {
		m.stopErr = m.stop(ctx)
	})
	return m.stopErr
}

func (m *Managed) waitReady(ctx context.Context, timeout time.Duration) error {
	client := &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	check := func() (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/health", nil)
		if err != nil {
			return false, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode == http.StatusOK, nil
	}

	for {
		if ok, err := check(); err != nil {
			return fmt.Errorf("check llama-server health: %w", err)
		} else if ok {
			return nil
		}

		select {
		case <-m.done:
			return fmt.Errorf("llama-server exited during startup: %v\n%s", m.WaitError(), strings.TrimSpace(m.Tail()))
		case <-ctx.Done():
			return fmt.Errorf("llama-server did not become ready within %s: %w\n%s", timeout, ctx.Err(), strings.TrimSpace(m.Tail()))
		case <-deadline.C:
			return fmt.Errorf("llama-server did not become ready within %s\n%s", timeout, strings.TrimSpace(m.Tail()))
		case <-ticker.C:
		}
	}
}

func (m *Managed) stop(ctx context.Context) error {
	select {
	case <-m.done:
		return nil
	default:
	}

	if err := interruptProcess(m.cmd.Process.Pid); err != nil {
		return m.forceStop()
	}

	wait := time.NewTimer(5 * time.Second)
	defer wait.Stop()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return m.forceStop()
	case <-wait.C:
	}

	if err := m.forceStop(); err != nil {
		return fmt.Errorf("stop llama-server: %w", err)
	}
	return nil
}

func (m *Managed) forceStop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	select {
	case <-m.done:
		return nil
	default:
	}
	if err := killProcess(m.cmd.Process.Pid); err != nil {
		_ = m.cmd.Process.Kill()
	}
	select {
	case <-m.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("llama-server did not exit after kill")
	}
}

func resolveExecutable(path string) (string, error) {
	if strings.ContainsAny(path, `/\`) || filepath.IsAbs(path) {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("llama-server executable: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("llama-server executable is a directory: %s", path)
		}
		return path, nil
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("find llama-server executable %q: %w", path, err)
	}
	return resolved, nil
}

func hostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

type logRing struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newLogRing(max int) *logRing {
	return &logRing{max: max}
}

func (r *logRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.max:]...)
	}
	return len(p), nil
}

func (r *logRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
