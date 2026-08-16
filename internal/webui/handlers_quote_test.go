// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/micasa-dev/micasa/internal/data"
)

// quoteFixture creates a project and vendor for tests that need a valid
// Quote.ProjectID / VendorID pair.
func quoteFixture(t *testing.T, srv *Server) (project data.Project, vendor data.Vendor) {
	t.Helper()
	project = data.Project{Title: "Fixture Project", ProjectTypeID: firstProjectTypeID(t, srv)}
	require.NoError(t, srv.store.CreateProject(&project))
	vendor = data.Vendor{Name: "Fixture Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	return project, vendor
}

func TestQuoteList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, quotesURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No quotes yet.")
}

func TestQuoteNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, quotesURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Quote")
}

func TestQuoteForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)

	form := url.Values{
		"project_id": {project.ID},
		"vendor_id":  {vendor.ID},
		"total":      {"1250.00"},
		"labor":      {"800.00"},
	}
	rec := do(t, srv, http.MethodPost, quotesURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, quotesURL, rec.Header().Get("Location"))

	quotes, err := srv.store.ListQuotes(false)
	require.NoError(t, err)
	require.Len(t, quotes, 1)
	require.Equal(t, project.ID, quotes[0].ProjectID)
	require.Equal(t, vendor.ID, quotes[0].VendorID)
	require.Equal(t, int64(125000), quotes[0].TotalCents)
}

func TestQuoteForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)
	quote := data.Quote{ProjectID: project.ID, TotalCents: 10000}
	require.NoError(t, srv.store.CreateQuote(&quote, vendor))

	form := url.Values{
		"project_id": {project.ID},
		"vendor_id":  {vendor.ID},
		"total":      {"2000.00"},
	}
	rec := do(t, srv, http.MethodPost, quotesURL+"/"+quote.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetQuote(quote.ID)
	require.NoError(t, err)
	require.Equal(t, int64(200000), updated.TotalCents)
}

func TestQuoteForm_MissingTotalRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)

	rec := do(t, srv, http.MethodPost, quotesURL, url.Values{
		"project_id": {project.ID}, "vendor_id": {vendor.ID},
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	quotes, err := srv.store.ListQuotes(false)
	require.NoError(t, err)
	require.Empty(t, quotes)
}

func TestQuoteForm_MissingProjectRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, quotesURL, url.Values{"total": {"100.00"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "project is required")

	quotes, err := srv.store.ListQuotes(false)
	require.NoError(t, err)
	require.Empty(t, quotes)
}

func TestQuoteDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)
	quote := data.Quote{ProjectID: project.ID, TotalCents: 5000}
	require.NoError(t, srv.store.CreateQuote(&quote, vendor))

	deleteRec := do(t, srv, http.MethodPost, quotesURL+"/"+quote.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListQuotes(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, quotesURL+"/"+quote.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListQuotes(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestQuoteEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)
	quote := data.Quote{ProjectID: project.ID, TotalCents: 42000}
	require.NoError(t, srv.store.CreateQuote(&quote, vendor))

	rec := do(t, srv, http.MethodGet, quotesURL+"/"+quote.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="$420.00"`)
}

func TestQuoteEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	project, vendor := quoteFixture(t, srv)
	quote := data.Quote{ProjectID: project.ID, TotalCents: 1000}
	require.NoError(t, srv.store.CreateQuote(&quote, vendor))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, quotesURL+"/"+quote.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestQuoteEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, quotesURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestQuoteList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, quotesURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
