package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"paste-anywhere/server/internal/httpapi"
	"paste-anywhere/server/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run(args []string) error {
	return runCLI(args, os.Stdout, os.Stderr)
}
func serve(s *storage.Store, listen string, maxText int, maxImage int) error {
	if err := s.Cleanup(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: listen, Handler: httpapi.New(s, maxText, maxImage).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	log.Printf("listening on %s", listen)
	for {
		select {
		case err := <-result:
			return err
		case <-tick.C:
			if err := s.Cleanup(); err != nil {
				log.Printf("cleanup: %v", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				return err
			}
			if err := <-result; !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		}
	}
}
