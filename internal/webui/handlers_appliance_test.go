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

func TestApplianceList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, appliancesURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No appliances yet.")
}

func TestApplianceNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, appliancesURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Appliance")
}

func TestApplianceForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"name":            {"Kitchen Refrigerator"},
		"brand":           {"Coldwell"},
		"model_number":    {"CW-100"},
		"purchase_date":   {"2020-01-15"},
		"warranty_expiry": {"2025-01-15"},
		"cost":            {"899.00"},
	}
	rec := do(t, srv, http.MethodPost, appliancesURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, appliancesURL, rec.Header().Get("Location"))

	appliances, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Len(t, appliances, 1)
	require.Equal(t, "Kitchen Refrigerator", appliances[0].Name)
	require.Equal(t, "Coldwell", appliances[0].Brand)
	require.NotNil(t, appliances[0].WarrantyExpiry)
	require.Equal(t, int64(89900), *appliances[0].CostCents)
}

func TestApplianceForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Original"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))

	form := url.Values{"name": {"Renamed"}, "brand": {"NewBrand"}}
	rec := do(t, srv, http.MethodPost, appliancesURL+"/"+appliance.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetAppliance(appliance.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, "NewBrand", updated.Brand)
}

func TestApplianceEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	cost := int64(50000)
	warranty := mustParseDate(t, "2026-06-01")
	appliance := data.Appliance{
		Name: "Prefill Dryer", CostCents: &cost, WarrantyExpiry: &warranty,
	}
	require.NoError(t, srv.store.CreateAppliance(&appliance))

	rec := do(t, srv, http.MethodGet, appliancesURL+"/"+appliance.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill Dryer"`)
	require.Contains(t, rec.Body.String(), `value="2026-06-01"`)
	require.Contains(t, rec.Body.String(), `value="$500.00"`)
}

func TestApplianceForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, appliancesURL, url.Values{"name": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "name is required")

	appliances, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Empty(t, appliances)
}

func TestApplianceForm_InvalidWarrantyDateRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, appliancesURL, url.Values{
		"name": {"Bad Date Appliance"}, "warranty_expiry": {"not-a-date"},
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Warranty Expiry should be")

	appliances, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Empty(t, appliances)
}

func TestApplianceDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Deletable Appliance"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))

	deleteRec := do(t, srv, http.MethodPost, appliancesURL+"/"+appliance.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(
		t,
		srv,
		http.MethodPost,
		appliancesURL+"/"+appliance.ID+"/restore",
		url.Values{},
	)
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestApplianceDelete_BlockedByDependentIncidentRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Appliance With Incident"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))
	require.NoError(t, srv.store.CreateIncident(&data.Incident{
		Title: "Fridge not cooling", ApplianceID: &appliance.ID,
	}))

	rec := do(t, srv, http.MethodPost, appliancesURL+"/"+appliance.ID+"/delete", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")

	appliances, err := srv.store.ListAppliances(false)
	require.NoError(t, err)
	require.Len(t, appliances, 1, "delete must be blocked while a dependent incident exists")
}

func TestApplianceEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	appliance := data.Appliance{Name: "Some Appliance"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, appliancesURL+"/"+appliance.ID+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestApplianceEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, appliancesURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
