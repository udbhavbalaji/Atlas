package main

import (
	"atlas/internal/httpapi"
	"atlas/internal/store"
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	path := flag.String("db", "data/atlas.db", "SQLite database path")
	gatewayKeyFile := flag.String("gateway-key-file", "data/ai-gateway.key", "Optional private file containing only the Vercel AI Gateway key; environment takes precedence")
	flag.Parse()
	if os.Getenv("AI_GATEWAY_API_KEY") == "" && *gatewayKeyFile != "" {
		key, err := os.ReadFile(*gatewayKeyFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Fatal("Could not read the configured Gateway key file")
		}
		if err == nil {
			value := strings.TrimSpace(string(key))
			if strings.ContainsAny(value, "\r\n\t ") {
				log.Fatal("Gateway key file must contain only the key")
			}
			if err = os.Setenv("AI_GATEWAY_API_KEY", value); err != nil {
				log.Fatal("Could not configure the Gateway key")
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(*path), 0700); err != nil {
		log.Fatal(err)
	}
	s, err := store.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	server := &http.Server{Addr: *addr, Handler: httpapi.Handler(s), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if err := s.ProcessDue(ctx, time.Now()); err != nil && ctx.Err() == nil {
				log.Printf("reminder delivery failed; will retry: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Print(err)
		}
	}()
	log.Printf("Atlas listening on http://%s", *addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	stop()
	<-shutdownDone
	<-workerDone
}
