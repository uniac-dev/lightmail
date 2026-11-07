package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/uniac-dev/mail-sender/internal/sender"
)

var version = "0.1.0"

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	retention, err := strconv.Atoi(env("SENDER_RETENTION_DAYS", "30"))
	if err != nil || retention < 1 || retention > 3650 {
		log.Fatal("SENDER_RETENTION_DAYS must be between 1 and 3650")
	}
	requireTLS, err := strconv.ParseBool(env("SENDER_REQUIRE_TLS", "true"))
	if err != nil {
		log.Fatal("SENDER_REQUIRE_TLS must be true or false")
	}
	app, err := sender.New(sender.Config{
		Address: env("SENDER_ADDRESS", ":8080"), DataDir: env("SENDER_DATA_DIR", "./data"),
		AdminToken: os.Getenv("SENDER_ADMIN_TOKEN"), Hostname: env("SENDER_HOSTNAME", "mail.example.com"),
		PublicIP: os.Getenv("SENDER_PUBLIC_IP"), EnvelopeFrom: os.Getenv("SENDER_ENVELOPE_FROM"),
		RequireTLS: requireTLS, Retention: time.Duration(retention) * 24 * time.Hour, Version: version,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Addr: env("SENDER_ADDRESS", ":8080"), Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 * 1024}
	errs := make(chan error, 2)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); errs <- app.RunWorker(ctx) }()
	go func() {
		log.Printf("Mail Sender %s listening on %s", version, server.Addr)
		errs <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("service stopped: %v", err)
		}
		cancel()
	}
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
	cancel()
	// Wait for the worker to leave its store transaction before closing the database.
	select {
	case <-workerDone:
	case <-shutdown.Done():
	}
}
