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

func firstMaintenanceCategoryID(t *testing.T, srv *Server) string {
	t.Helper()
	cats, err := srv.store.MaintenanceCategories()
	require.NoError(t, err)
	require.NotEmpty(t, cats)
	return cats[0].ID
}

func TestMaintenanceList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, maintenanceURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No maintenance items yet.")
}

func TestMaintenanceNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, maintenanceURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Maintenance Item")
}

func TestMaintenanceForm_CreateWithNoSchedule(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)

	form := url.Values{
		"name":          {"Clean gutters"},
		"category_id":   {categoryID},
		"schedule_type": {"none"},
	}
	rec := do(t, srv, http.MethodPost, maintenanceURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "Clean gutters", items[0].Name)
	require.Zero(t, items[0].IntervalMonths)
	require.Nil(t, items[0].DueDate)
}

func TestMaintenanceForm_CreateWithInterval(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)

	form := url.Values{
		"name":            {"Replace HVAC filter"},
		"category_id":     {categoryID},
		"schedule_type":   {"interval"},
		"interval_months": {"3"},
	}
	rec := do(t, srv, http.MethodPost, maintenanceURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 3, items[0].IntervalMonths)
	require.Nil(t, items[0].DueDate)
}

func TestMaintenanceForm_CreateWithDueDate(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)

	form := url.Values{
		"name":          {"Chimney inspection"},
		"category_id":   {categoryID},
		"schedule_type": {"due_date"},
		"due_date":      {"2026-12-01"},
	}
	rec := do(t, srv, http.MethodPost, maintenanceURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Zero(t, items[0].IntervalMonths)
	require.NotNil(t, items[0].DueDate)
}

func TestMaintenanceForm_IgnoresIntervalFieldWhenScheduleTypeIsDueDate(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)

	// interval_months is present but schedule_type says due_date, so the
	// TUI-matching behavior is to ignore interval_months entirely.
	form := url.Values{
		"name":            {"Mixed fields"},
		"category_id":     {categoryID},
		"schedule_type":   {"due_date"},
		"due_date":        {"2026-12-01"},
		"interval_months": {"6"},
	}
	rec := do(t, srv, http.MethodPost, maintenanceURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Zero(t, items[0].IntervalMonths)
}

func TestMaintenanceForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Original", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	form := url.Values{
		"name":          {"Renamed"},
		"category_id":   {categoryID},
		"schedule_type": {"none"},
	}
	rec := do(t, srv, http.MethodPost, maintenanceURL+"/"+item.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetMaintenance(item.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
}

func TestMaintenanceForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, maintenanceURL, url.Values{"name": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "name is required")

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestMaintenanceDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Deletable Item", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	deleteRec := do(t, srv, http.MethodPost, maintenanceURL+"/"+item.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, maintenanceURL+"/"+item.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestMaintenanceDelete_BlockedByDependentServiceLogRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Item With Log", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	require.NoError(t, srv.store.CreateServiceLog(&data.ServiceLogEntry{
		MaintenanceItemID: item.ID, ServicedAt: mustParseDate(t, "2026-01-01"),
	}, data.Vendor{}))

	rec := do(t, srv, http.MethodPost, maintenanceURL+"/"+item.ID+"/delete", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")

	items, err := srv.store.ListMaintenance(false)
	require.NoError(t, err)
	require.Len(t, items, 1, "delete must be blocked while a dependent service log exists")
}

func TestMaintenanceEditForm_PrefillsIntervalSchedule(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{
		Name: "Prefill Interval Item", CategoryID: categoryID, IntervalMonths: 6,
	}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	rec := do(t, srv, http.MethodGet, maintenanceURL+"/"+item.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill Interval Item"`)
	require.Contains(t, rec.Body.String(), `value="interval" checked`)
	require.Contains(t, rec.Body.String(), `value="6"`)
}

func TestMaintenanceEditForm_PrefillsDueDateSchedule(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	due := mustParseDate(t, "2026-12-01")
	item := data.MaintenanceItem{
		Name: "Prefill Due Date Item", CategoryID: categoryID, DueDate: &due,
	}
	require.NoError(t, srv.store.CreateMaintenance(&item))

	rec := do(t, srv, http.MethodGet, maintenanceURL+"/"+item.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="due_date" checked`)
	require.Contains(t, rec.Body.String(), `value="2026-12-01"`)
}

func TestMaintenanceEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	categoryID := firstMaintenanceCategoryID(t, srv)
	item := data.MaintenanceItem{Name: "Some Item", CategoryID: categoryID}
	require.NoError(t, srv.store.CreateMaintenance(&item))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, maintenanceURL+"/"+item.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMaintenanceEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, maintenanceURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMaintenanceList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, maintenanceURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
