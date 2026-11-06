package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/marvelousu/local-jp-translate/internal/access"
	"github.com/marvelousu/local-jp-translate/internal/backend"
	"github.com/marvelousu/local-jp-translate/internal/httpapi"
	"github.com/marvelousu/local-jp-translate/internal/translate"
)

const defaultModelName = "plamo-3-translate-0.8b-Q4_K_M"

const defaultLlamaContext = 16384

func runServe(args []string) error {
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	return serveWithContext(signalCtx, args)
}

func serveWithContext(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	addr := fs.String("addr", "127.0.0.1:8787", "loopback listen address")
	backendURL := fs.String("backend", "http://127.0.0.1:8000", "existing loopback llama-server URL")
	llamaServer := fs.String("llama-server", "", "path to llama-server to launch and manage")
	modelPath := fs.String("model", "", "GGUF model path (required with --llama-server)")
	llamaHost := fs.String("llama-host", "127.0.0.1", "managed llama-server loopback host")
	llamaPort := fs.Int("llama-port", 8788, "managed llama-server port")
	llamaContext := fs.Int("llama-context", defaultLlamaContext, "managed llama-server context size (256..32768)")
	llamaAlias := fs.String("llama-alias", "plamo-3-translate", "managed llama-server model alias")
	startupTimeout := fs.Duration("startup-timeout", 180*time.Second, "managed llama-server readiness timeout")
	requestTimeout := fs.Duration("request-timeout", 180*time.Second, "per-translation timeout including queue wait")
	queueSize := fs.Int("queue-size", 8, "maximum waiting requests beyond the active inference slot")
	modelName := fs.String("model-name", "", "model name reported in responses")
	maxBody := fs.Int64("max-body-bytes", 1<<20, "maximum request body size")
	var corsOrigins stringList
	fs.Var(&corsOrigins, "cors-origin", "explicit browser origin to allow (repeatable; default disabled)")
	fs.Var(&corsOrigins, "allow-origin", "alias of --cors-origin (repeatable exact origins)")
	var llamaArgs stringList
	fs.Var(&llamaArgs, "llama-arg", "extra llama-server argument (repeatable; one argument per flag)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if err := backend.ValidateListenAddress(*addr); err != nil {
		return err
	}
	if _, err := access.Wrap(http.NotFoundHandler(), corsOrigins); err != nil {
		return err
	}

	managed := *llamaServer != ""
	hasBackend := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "backend" {
			hasBackend = true
		}
	})
	if managed && hasBackend {
		return errors.New("--backend and --llama-server are mutually exclusive")
	}
	if managed && *modelPath == "" {
		return errors.New("--model is required with --llama-server")
	}
	if !managed && *modelPath != "" {
		return errors.New("--model is only used with --llama-server")
	}
	if *queueSize < 0 {
		return errors.New("--queue-size must be zero or greater")
	}
	if *requestTimeout <= 0 {
		return errors.New("--request-timeout must be positive")
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	var client *backend.Client
	var managedServer *backend.Managed
	if managed {
		var err error
		managedServer, err = backend.StartManaged(ctx, backend.ManagedConfig{
			Executable:     *llamaServer,
			Model:          *modelPath,
			Host:           *llamaHost,
			Port:           *llamaPort,
			ContextSize:    *llamaContext,
			Alias:          *llamaAlias,
			ExtraArgs:      llamaArgs,
			StartupTimeout: *startupTimeout,
			LogWriter:      os.Stderr,
		})
		if err != nil {
			return err
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := managedServer.Stop(stopCtx); err != nil {
				logger.Printf("stop llama-server: %v", err)
			}
		}()
		client, err = backend.NewClient(managedServer.BaseURL(), nil)
		if err != nil {
			return err
		}
		logger.Printf("managed llama-server ready at %s", managedServer.BaseURL())
	} else {
		var err error
		client, err = backend.NewClient(*backendURL, nil)
		if err != nil {
			return err
		}
		healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		healthErr := client.Health(healthCtx)
		cancel()
		if healthErr != nil {
			logger.Printf("warning: backend is not ready yet at %s: %v", client.BaseURL(), healthErr)
		} else {
			logger.Printf("using external llama-server at %s", client.BaseURL())
		}
	}

	name := *modelName
	if name == "" {
		name = defaultModelName
		if *modelPath != "" {
			name = strings.TrimSuffix(filepath.Base(*modelPath), filepath.Ext(*modelPath))
		}
	}

	service := translate.NewService(translate.Config{
		Model:          name,
		RequestTimeout: *requestTimeout,
		QueueSize:      *queueSize,
	}, client)

	api := httpapi.New(httpapi.Options{
		Service:     service,
		Version:     version,
		MaxBody:     *maxBody,
		CORSOrigins: corsOrigins,
		Logger:      logger,
	})
	protectedAPI, err := access.Wrap(api, corsOrigins)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}

	server := &http.Server{
		Handler:           protectedAPI,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	logger.Printf("listening on http://%s (model %s, queue %d)", listener.Addr().String(), name, *queueSize)
	var managedDone <-chan struct{}
	if managedServer != nil {
		managedDone = managedServer.Done()
	}
	var backendFailure error

	select {
	case <-ctx.Done():
		logger.Printf("shutting down")
	case <-managedDone:
		backendFailure = fmt.Errorf("managed llama-server exited: %v", managedServer.WaitError())
		logger.Printf("%v\n%s", backendFailure, strings.TrimSpace(managedServer.Tail()))
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return backendFailure
}
