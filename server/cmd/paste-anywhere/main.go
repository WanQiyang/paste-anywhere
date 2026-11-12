package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	if len(args) == 0 {
		return fmt.Errorf("usage: paste-anywhere <serve|user-create|user-list|user-reset|user-delete|device-list|device-delete> [flags]")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	db := f.String("db", "data/paste-anywhere.db", "SQLite file (use the same file for CLI and serve)")
	listen := f.String("listen", "127.0.0.1:8080", "HTTP address; terminate HTTPS at reverse proxy")
	name := f.String("name", "", "user nickname")
	id := f.String("id", "", "user/device ID")
	history := f.Int("history", 20, "maximum entries per user")
	retention := f.Duration("retention", 7*24*time.Hour, "entry lifetime")
	maxText := f.Int("max-text", 1024*1024, "maximum UTF-8 text bytes")
	maxImage := f.Int("max-image", 16*1024*1024, "maximum PNG/JPEG bytes")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *maxText < 1 || *maxText > 16*1024*1024 {
		return fmt.Errorf("max-text must be 1..16777216 bytes")
	}
	if *maxImage < 1 || *maxImage > 64*1024*1024 {
		return fmt.Errorf("max-image must be 1..67108864 bytes")
	}
	s, err := storage.Open(*db, *history, *retention)
	if err != nil {
		return err
	}
	defer s.Close()
	switch args[0] {
	case "user-create":
		*name = strings.TrimSpace(*name)
		if len(*name) == 0 || len(*name) > 128 {
			return fmt.Errorf("name must be 1..128 UTF-8 bytes")
		}
		u, invite, e := s.CreateUser(*name)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			User   storage.User `json:"user"`
			Invite string       `json:"invite"`
		}{u, invite})
	case "user-list":
		v, e := s.Users()
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "device-list":
		v, e := s.Devices()
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "user-reset":
		v, e := s.ResetInvite(*id)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"invite": v})
	case "user-delete":
		return s.DeleteUser(*id)
	case "device-delete":
		return s.DeleteDevice(*id)
	case "serve":
		return serve(s, *listen, *maxText, *maxImage)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
