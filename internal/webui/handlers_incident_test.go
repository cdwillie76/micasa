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

func TestIncidentList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, incidentsURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No incidents yet.")
}

func TestIncidentNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, incidentsURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Incident")
}

func TestIncidentForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"title":        {"Garage door won't open"},
		"severity":     {data.IncidentSeverityUrgent},
		"date_noticed": {"2026-08-01"},
	}
	rec := do(t, srv, http.MethodPost, incidentsURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, incidentsURL, rec.Header().Get("Location"))

	incidents, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Len(t, incidents, 1)
	require.Equal(t, "Garage door won't open", incidents[0].Title)
	require.Equal(t, data.IncidentStatusOpen, incidents[0].Status, "new incidents default to open")
	require.Equal(t, data.IncidentSeverityUrgent, incidents[0].Severity)
	require.Nil(t, incidents[0].ApplianceID)
	require.Nil(t, incidents[0].VendorID)
}

func TestIncidentForm_CreateWithApplianceAndVendorLinks(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Garage Door Opener"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))
	vendor := data.Vendor{Name: "Door Repair Co"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	form := url.Values{
		"title":        {"Opener motor burned out"},
		"severity":     {data.IncidentSeverityUrgent},
		"date_noticed": {"2026-08-01"},
		"appliance_id": {appliance.ID},
		"vendor_id":    {vendor.ID},
	}
	rec := do(t, srv, http.MethodPost, incidentsURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	incidents, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Len(t, incidents, 1)
	require.Equal(t, appliance.ID, *incidents[0].ApplianceID)
	require.Equal(t, vendor.ID, *incidents[0].VendorID)
}

func TestIncidentForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	incident := data.Incident{
		Title: "Original", Status: data.IncidentStatusOpen,
		Severity: data.IncidentSeveritySoon, DateNoticed: mustParseDate(t, "2026-08-01"),
	}
	require.NoError(t, srv.store.CreateIncident(&incident))

	form := url.Values{
		"title":        {"Updated Title"},
		"status":       {data.IncidentStatusResolved},
		"severity":     {data.IncidentSeveritySoon},
		"date_noticed": {"2026-08-01"},
	}
	rec := do(t, srv, http.MethodPost, incidentsURL+"/"+incident.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetIncident(incident.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", updated.Title)
	require.Equal(t, data.IncidentStatusResolved, updated.Status)
}

func TestIncidentForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, incidentsURL, url.Values{"title": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "title is required")

	incidents, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Empty(t, incidents)
}

func TestIncidentForm_MissingDateNoticedRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, incidentsURL, url.Values{"title": {"No Date Incident"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Date Noticed should be")

	incidents, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Empty(t, incidents)
}

func TestIncidentDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	incident := data.Incident{
		Title: "Deletable Incident", Status: data.IncidentStatusOpen,
		Severity: data.IncidentSeveritySoon, DateNoticed: mustParseDate(t, "2026-08-01"),
	}
	require.NoError(t, srv.store.CreateIncident(&incident))

	deleteRec := do(t, srv, http.MethodPost, incidentsURL+"/"+incident.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, incidentsURL+"/"+incident.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListIncidents(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(
		t, data.IncidentStatusOpen, restored[0].Status,
		"restore should return the incident to its pre-delete status",
	)
}

func TestIncidentEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	incident := data.Incident{
		Title: "Some Incident", Status: data.IncidentStatusOpen,
		Severity: data.IncidentSeveritySoon, DateNoticed: mustParseDate(t, "2026-01-01"),
	}
	require.NoError(t, srv.store.CreateIncident(&incident))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, incidentsURL+"/"+incident.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestIncidentEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, incidentsURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestIncidentEditForm_PrefillsSelectedApplianceAndVendor(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Water Heater"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))
	vendor := data.Vendor{Name: "Plumbing Co"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	incident := data.Incident{
		Title: "Leaking tank", Status: data.IncidentStatusOpen,
		Severity: data.IncidentSeverityUrgent, DateNoticed: mustParseDate(t, "2026-08-01"),
		ApplianceID: &appliance.ID, VendorID: &vendor.ID,
	}
	require.NoError(t, srv.store.CreateIncident(&incident))

	rec := do(t, srv, http.MethodGet, incidentsURL+"/"+incident.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="`+appliance.ID+`" selected`)
	require.Contains(t, rec.Body.String(), `value="`+vendor.ID+`" selected`)
}

func TestIncidentList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, incidentsURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
