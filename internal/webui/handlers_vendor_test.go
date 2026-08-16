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

func TestVendorList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, vendorsURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No vendors yet.")
}

func TestVendorNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, vendorsURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Vendor")
}

func TestVendorForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"name":         {"Acme Plumbing"},
		"contact_name": {"Jane Roe"},
		"email":        {"jane@acme.example"},
		"phone":        {"555-1234"},
		"website":      {"https://acme.example"},
		"notes":        {"Reliable"},
	}
	rec := do(t, srv, http.MethodPost, vendorsURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, vendorsURL, rec.Header().Get("Location"))

	vendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Len(t, vendors, 1)
	require.Equal(t, "Acme Plumbing", vendors[0].Name)
	require.Equal(t, "Jane Roe", vendors[0].ContactName)

	listRec := do(t, srv, http.MethodGet, vendorsURL, nil)
	require.Contains(t, listRec.Body.String(), "Acme Plumbing")
}

func TestVendorForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Original Name"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	form := url.Values{"name": {"Updated Name"}, "phone": {"555-9999"}}
	rec := do(t, srv, http.MethodPost, vendorsURL+"/"+vendor.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetVendor(vendor.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Name", updated.Name)
	require.Equal(t, "555-9999", updated.Phone)
}

func TestVendorEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Prefill Vendor", Email: "vendor@example.com"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	rec := do(t, srv, http.MethodGet, vendorsURL+"/"+vendor.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill Vendor"`)
	require.Contains(t, rec.Body.String(), `value="vendor@example.com"`)
}

func TestVendorEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, vendorsURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestVendorForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, vendorsURL, url.Values{"name": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "name is required")

	vendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Empty(t, vendors)
}

func TestVendorForm_DuplicateNameSurfacesStoreError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateVendor(&data.Vendor{Name: "Dup Vendor"}))

	rec := do(t, srv, http.MethodPost, vendorsURL, url.Values{"name": {"Dup Vendor"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	vendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Len(t, vendors, 1, "duplicate submission must not create a second vendor")
}

func TestVendorDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Deletable Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	deleteRec := do(t, srv, http.MethodPost, vendorsURL+"/"+vendor.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	activeVendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Empty(t, activeVendors)

	allVendors, err := srv.store.ListVendors(true)
	require.NoError(t, err)
	require.Len(t, allVendors, 1)
	require.True(t, allVendors[0].DeletedAt.Valid)

	restoreRec := do(t, srv, http.MethodPost, vendorsURL+"/"+vendor.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restoredVendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Len(t, restoredVendors, 1)
	require.False(t, restoredVendors[0].DeletedAt.Valid)
}

func TestVendorDelete_BlockedByDependentIncidentRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Vendor With Incident"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	require.NoError(t, srv.store.CreateIncident(&data.Incident{
		Title: "Leaky faucet", VendorID: &vendor.ID,
	}))

	rec := do(t, srv, http.MethodPost, vendorsURL+"/"+vendor.ID+"/delete", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	location := rec.Header().Get("Location")
	require.Contains(t, location, "error=")

	vendors, err := srv.store.ListVendors(false)
	require.NoError(t, err)
	require.Len(t, vendors, 1, "delete must be blocked while a dependent incident exists")

	listRec := do(t, srv, http.MethodGet, location, nil)
	require.Equal(t, http.StatusOK, listRec.Code)
	require.Contains(t, listRec.Body.String(), "active incident")
}

func TestVendorRestore_StoreErrorRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodPost, vendorsURL+"/does-not-exist/restore", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")
}

func TestVendorForm_UpdateValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Keep This Name"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	rec := do(t, srv, http.MethodPost, vendorsURL+"/"+vendor.ID, url.Values{"name": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "name is required")

	unchanged, err := srv.store.GetVendor(vendor.ID)
	require.NoError(t, err)
	require.Equal(t, "Keep This Name", unchanged.Name)
}

func TestVendorForm_UpdateStoreErrorSurfacesMessage(t *testing.T) {
	srv := newTestServer(t)
	first := data.Vendor{Name: "First Vendor"}
	second := data.Vendor{Name: "Second Vendor"}
	require.NoError(t, srv.store.CreateVendor(&first))
	require.NoError(t, srv.store.CreateVendor(&second))

	// Renaming "second" to "first"'s name violates the unique index.
	rec := do(
		t,
		srv,
		http.MethodPost,
		vendorsURL+"/"+second.ID,
		url.Values{"name": {"First Vendor"}},
	)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	unchanged, err := srv.store.GetVendor(second.ID)
	require.NoError(t, err)
	require.Equal(t, "Second Vendor", unchanged.Name)
}

func TestVendorList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, vendorsURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVendorEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Some Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, vendorsURL+"/"+vendor.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVendorList_ShowDeletedToggle(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Hidden Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	require.NoError(t, srv.store.DeleteVendor(vendor.ID))

	defaultRec := do(t, srv, http.MethodGet, vendorsURL, nil)
	require.NotContains(t, defaultRec.Body.String(), "Hidden Vendor")

	deletedRec := do(t, srv, http.MethodGet, vendorsURL+"?deleted=1", nil)
	require.Contains(t, deletedRec.Body.String(), "Hidden Vendor")
}
