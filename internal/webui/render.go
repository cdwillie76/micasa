// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
)

// pageData is embedded in every page's template data to drive the shared
// layout (nav highlighting, page title, container width).
type pageData struct {
	Title string
	Nav   string // matches a nav item's identifier for the active-link style

	// Wide widens the page container for table/grid-heavy pages (entity
	// lists, the dashboard). List and form pages share the same Nav value,
	// so this can't be derived from Nav -- it's set explicitly by each
	// handler that renders a table.
	Wide bool
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

// redirectWithError redirects back to target with err's message attached as
// a query parameter, for operations (like a dependency-blocked delete) whose
// failure should be shown on the page the user came from rather than as a
// bare 500. target may already carry its own query string.
func redirectWithErrorTarget(target string, err error) string {
	u, parseErr := url.Parse(target)
	if parseErr != nil {
		return target
	}
	q := u.Query()
	q.Set("error", err.Error())
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Server) redirectWithError(
	w http.ResponseWriter,
	r *http.Request,
	target string,
	err error,
) {
	http.Redirect(w, r, redirectWithErrorTarget(target, err), http.StatusSeeOther)
}
