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
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	path := flag.String("db", "data/atlas.db", "SQLite database path")
	flag.Parse()
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
	go func() {
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
}
