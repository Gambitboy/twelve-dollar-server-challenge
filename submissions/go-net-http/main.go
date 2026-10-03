// Command feedapi is the reference submission (see SPEC.md at the repo root): the Go +
// SQLite build from the video, unoptimized on purpose. Beat it.
//
// Standard library net/http with Go 1.22+ pattern routing, encoding/json,
// database/sql + mattn/go-sqlite3 (cgo) for SQLite and golang-jwt/jwt/v5 for HS256 bearer tokens.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type config struct {
	sqlitePath string
	jwtSecret  []byte
	poolSize   int
	host       string
	port       string
}

func loadConfig() config {
	cfg := config{
		sqlitePath: os.Getenv("SQLITE_PATH"),
		jwtSecret:  []byte(os.Getenv("JWT_SECRET")),
		poolSize:   10,
		host:       envOr("HOST", "127.0.0.1"),
		port:       envOr("PORT", "3000"),
	}
	if cfg.sqlitePath == "" || len(cfg.jwtSecret) == 0 {
		log.Fatal("SQLITE_PATH and JWT_SECRET must be set")
	}
	return cfg
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg := loadConfig()

	// The PRAGMAs go in the DSN: go-sqlite3 runs them on every new connection it opens,
	// so every connection in the database/sql pool has them.
	dsn := "file:" + (&url.URL{Path: cfg.sqlitePath}).EscapedPath() +
		"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatalf("open %s: %v", cfg.sqlitePath, err)
	}
	// database/sql defaults to unlimited open / 2 idle connections; the video used a pool of 10.
	db.SetMaxOpenConns(cfg.poolSize)
	db.SetMaxIdleConns(cfg.poolSize)
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("open %s: %v", cfg.sqlitePath, err)
	}

	app := &app{db: db, jwtSecret: cfg.jwtSecret, started: time.Now()}

	srv := &http.Server{
		Addr:    net.JoinHostPort(cfg.host, cfg.port),
		Handler: app.routes(),
		// Outlive Nginx's upstream keep-alive so it never reuses a socket we just closed.
		IdleTimeout:       65 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.Print("[server] shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("[server] listening on http://%s (pid %d)", srv.Addr, os.Getpid())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
}
