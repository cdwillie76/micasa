// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"bytes"
	"fmt"
	"net/http"
)

// pageData is embedded in every page's template data to drive the shared
// layout (nav highlighting, page title).
type pageData struct {
	Title string
	Nav   string // matches a nav item's identifier for the active-link style
}

// render executes the named page template into a buffer first, so a
// template execution error produces a proper 500 instead of a
// half-written response with a 200 already sent.
func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.tmpl.pages[page]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown template %q", page), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		http.Error(w, fmt.Sprintf("render %s: %v", page, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderError surfaces a store/query failure as a 500 with the error text.
// Every caller has already wrapped err with the operation that failed
// (fmt.Errorf("load X: %w", err)), so the plain message is actionable.
func (s *Server) renderError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
