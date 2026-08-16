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

func TestServiceLogList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, serviceLogURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No service log entries yet.")
}

func TestServiceLogNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, serviceLogURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Service Log Entry")
}

func TestServiceLogForm_CreateWithoutVendor(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	form := url.Values{
		"maintenance_item_id": {item.ID},
		"serviced_at":         {"2026-03-01"},
		"cost":                {"150.00"},
	}
	rec := do(t, srv, http.MethodPost, serviceLogURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	entries, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, item.ID, entries[0].MaintenanceItemID)
	require.Nil(t, entries[0].VendorID)
	require.Equal(t, int64(15000), *entries[0].CostCents)
}

func TestServiceLogForm_CreateWithVendor(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	vendor := data.Vendor{Name: "HVAC Pros"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	form := url.Values{
		"maintenance_item_id": {item.ID},
		"vendor_id":           {vendor.ID},
		"serviced_at":         {"2026-03-01"},
	}
	rec := do(t, srv, http.MethodPost, serviceLogURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	entries, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, vendor.ID, *entries[0].VendorID)
}

func TestServiceLogForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	entry := data.ServiceLogEntry{
		MaintenanceItemID: item.ID,
		ServicedAt:        mustParseDate(t, "2026-01-01"),
	}
	require.NoError(t, srv.store.CreateServiceLog(&entry, data.Vendor{}))

	form := url.Values{
		"maintenance_item_id": {item.ID},
		"serviced_at":         {"2026-02-15"},
	}
	rec := do(t, srv, http.MethodPost, serviceLogURL+"/"+entry.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetServiceLog(entry.ID)
	require.NoError(t, err)
	require.Equal(t, "2026-02-15", updated.ServicedAt.Format("2006-01-02"))
}

func TestServiceLogForm_MissingMaintenanceItemRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, serviceLogURL, url.Values{"serviced_at": {"2026-01-01"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "maintenance item is required")

	entries, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestServiceLogForm_MissingServicedAtRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	rec := do(t, srv, http.MethodPost, serviceLogURL, url.Values{
		"maintenance_item_id": {item.ID},
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Serviced At should be")

	entries, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestServiceLogDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	entry := data.ServiceLogEntry{
		MaintenanceItemID: item.ID,
		ServicedAt:        mustParseDate(t, "2026-01-01"),
	}
	require.NoError(t, srv.store.CreateServiceLog(&entry, data.Vendor{}))

	deleteRec := do(t, srv, http.MethodPost, serviceLogURL+"/"+entry.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, serviceLogURL+"/"+entry.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListAllServiceLogEntries(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestServiceLogEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	entry := data.ServiceLogEntry{
		MaintenanceItemID: item.ID,
		ServicedAt:        mustParseDate(t, "2026-04-15"),
	}
	require.NoError(t, srv.store.CreateServiceLog(&entry, data.Vendor{}))

	rec := do(t, srv, http.MethodGet, serviceLogURL+"/"+entry.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="2026-04-15"`)
}

func TestServiceLogEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Furnace", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	entry := data.ServiceLogEntry{
		MaintenanceItemID: item.ID,
		ServicedAt:        mustParseDate(t, "2026-04-15"),
	}
	require.NoError(t, srv.store.CreateServiceLog(&entry, data.Vendor{}))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, serviceLogURL+"/"+entry.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServiceLogEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, serviceLogURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServiceLogList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, serviceLogURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
