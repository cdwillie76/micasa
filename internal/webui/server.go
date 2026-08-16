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

	mux.HandleFunc("GET /appliances", s.handleApplianceList)
	mux.HandleFunc("GET /appliances/new", s.handleApplianceNewForm)
	mux.HandleFunc("POST /appliances", s.handleApplianceCreate)
	mux.HandleFunc("GET /appliances/{id}/edit", s.handleApplianceEditForm)
	mux.HandleFunc("POST /appliances/{id}", s.handleApplianceUpdate)
	mux.HandleFunc("POST /appliances/{id}/delete", s.handleApplianceDelete)
	mux.HandleFunc("POST /appliances/{id}/restore", s.handleApplianceRestore)

	mux.HandleFunc("GET /projects", s.handleProjectList)
	mux.HandleFunc("GET /projects/new", s.handleProjectNewForm)
	mux.HandleFunc("POST /projects", s.handleProjectCreate)
	mux.HandleFunc("GET /projects/{id}/edit", s.handleProjectEditForm)
	mux.HandleFunc("POST /projects/{id}", s.handleProjectUpdate)
	mux.HandleFunc("POST /projects/{id}/delete", s.handleProjectDelete)
	mux.HandleFunc("POST /projects/{id}/restore", s.handleProjectRestore)

	mux.HandleFunc("GET /incidents", s.handleIncidentList)
	mux.HandleFunc("GET /incidents/new", s.handleIncidentNewForm)
	mux.HandleFunc("POST /incidents", s.handleIncidentCreate)
	mux.HandleFunc("GET /incidents/{id}/edit", s.handleIncidentEditForm)
	mux.HandleFunc("POST /incidents/{id}", s.handleIncidentUpdate)
	mux.HandleFunc("POST /incidents/{id}/delete", s.handleIncidentDelete)
	mux.HandleFunc("POST /incidents/{id}/restore", s.handleIncidentRestore)

	mux.HandleFunc("GET /maintenance", s.handleMaintenanceList)
	mux.HandleFunc("GET /maintenance/new", s.handleMaintenanceNewForm)
	mux.HandleFunc("POST /maintenance", s.handleMaintenanceCreate)
	mux.HandleFunc("GET /maintenance/{id}/edit", s.handleMaintenanceEditForm)
	mux.HandleFunc("POST /maintenance/{id}", s.handleMaintenanceUpdate)
	mux.HandleFunc("POST /maintenance/{id}/delete", s.handleMaintenanceDelete)
	mux.HandleFunc("POST /maintenance/{id}/restore", s.handleMaintenanceRestore)

	mux.HandleFunc("GET /quotes", s.handleQuoteList)
	mux.HandleFunc("GET /quotes/new", s.handleQuoteNewForm)
	mux.HandleFunc("POST /quotes", s.handleQuoteCreate)
	mux.HandleFunc("GET /quotes/{id}/edit", s.handleQuoteEditForm)
	mux.HandleFunc("POST /quotes/{id}", s.handleQuoteUpdate)
	mux.HandleFunc("POST /quotes/{id}/delete", s.handleQuoteDelete)
	mux.HandleFunc("POST /quotes/{id}/restore", s.handleQuoteRestore)

	mux.HandleFunc("GET /service-logs", s.handleServiceLogList)
	mux.HandleFunc("GET /service-logs/new", s.handleServiceLogNewForm)
	mux.HandleFunc("POST /service-logs", s.handleServiceLogCreate)
	mux.HandleFunc("GET /service-logs/{id}/edit", s.handleServiceLogEditForm)
	mux.HandleFunc("POST /service-logs/{id}", s.handleServiceLogUpdate)
	mux.HandleFunc("POST /service-logs/{id}/delete", s.handleServiceLogDelete)
	mux.HandleFunc("POST /service-logs/{id}/restore", s.handleServiceLogRestore)

	mux.HandleFunc("GET /documents", s.handleDocumentList)
	mux.HandleFunc("GET /documents/new", s.handleDocumentNewForm)
	mux.HandleFunc("POST /documents", s.handleDocumentCreate)
	mux.HandleFunc("GET /documents/{id}/edit", s.handleDocumentEditForm)
	mux.HandleFunc("POST /documents/{id}", s.handleDocumentUpdate)
	mux.HandleFunc("POST /documents/{id}/delete", s.handleDocumentDelete)
	mux.HandleFunc("POST /documents/{id}/restore", s.handleDocumentRestore)
	mux.HandleFunc("GET /documents/{id}/download", s.handleDocumentDownload)

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
