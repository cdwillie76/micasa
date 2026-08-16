// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

// Package webui implements a local, single-user HTTP server that renders
// the same data.Store state the TUI (internal/app) renders to a terminal,
// as server-rendered HTML instead. See plans/web-interface.md for the
// design rationale. This package never imports internal/app: the two UIs
// are siblings over the shared data layer, not layered on each other.
package webui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/micasa-dev/micasa/internal/data"
	"github.com/micasa-dev/micasa/internal/locale"
)

// Server serves the local web UI. It holds no state of its own beyond the
// store and display preferences -- every request reads current state from
// the store, matching the TUI's "database is the source of truth" model.
type Server struct {
	store *data.Store
	cur   locale.Currency
	units data.UnitSystem
	tmpl  *templates
	mux   *http.ServeMux
}

// NewServer builds a Server wired to store. cur and units control money and
// area formatting/parsing, matching the values the TUI resolves at startup
// (Store.Currency() / Store.GetUnitSystem()).
func NewServer(store *data.Store, cur locale.Currency, units data.UnitSystem) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	s := &Server{store: store, cur: cur, units: units, tmpl: tmpl}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /house", s.handleHouseView)
	mux.HandleFunc("GET /house/edit", s.handleHouseEditForm)
	mux.HandleFunc("POST /house", s.handleHouseSubmit)

	mux.HandleFunc("GET /vendors", s.handleVendorList)
	mux.HandleFunc("GET /vendors/new", s.handleVendorNewForm)
	mux.HandleFunc("POST /vendors", s.handleVendorCreate)
	mux.HandleFunc("GET /vendors/{id}/edit", s.handleVendorEditForm)
	mux.HandleFunc("POST /vendors/{id}", s.handleVendorUpdate)
	mux.HandleFunc("POST /vendors/{id}/delete", s.handleVendorDelete)
	mux.HandleFunc("POST /vendors/{id}/restore", s.handleVendorRestore)

	s.mux = mux
}

// Run starts the server on addr and blocks until ctx is cancelled, then
// gracefully shuts down. Mirrors the shutdown pattern in cmd/relay/main.go.
func (s *Server) Run(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	// ctx is already Done here, so derive the shutdown deadline from a
	// decoupled copy rather than context.Background() (satisfies contextcheck
	// while still not inheriting the cancellation we're reacting to).
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down server: %w", err)
	}
	return nil
}
