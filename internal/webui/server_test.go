// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/micasa-dev/micasa/internal/data"
	"github.com/micasa-dev/micasa/internal/locale"
)

// newTestServer builds a Server backed by a fresh in-memory store, matching
// how the TUI opens an in-memory database in demo mode (cmd/micasa's
// runDemo -> launchTUI(":memory:", ...)).
func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := data.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.AutoMigrate())
	require.NoError(t, store.SetMaxDocumentSize(50<<20))
	require.NoError(t, store.SeedDefaults())
	store.SetCurrency(locale.DefaultCurrency())
	t.Cleanup(func() { _ = store.Close() })

	srv, err := NewServer(store, store.Currency(), data.UnitsImperial)
	require.NoError(t, err)
	return srv
}

// firstProjectTypeID returns the ID of a seeded ProjectType, for tests that
// need a valid Project.ProjectTypeID (a required, non-nullable FK).
func firstProjectTypeID(t *testing.T, srv *Server) string {
	t.Helper()
	types, err := srv.store.ProjectTypes()
	require.NoError(t, err)
	require.NotEmpty(t, types)
	return types[0].ID
}

// mustParseDate parses a YYYY-MM-DD literal for test fixtures.
func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return tm
}

// do drives a request through the server exactly as a browser would --
// this package has no TabHandler-style internal API to call into directly,
// so every test exercises the real HTTP request/response cycle.
func do(
	t *testing.T,
	srv *Server,
	method, target string,
	form url.Values,
) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequestWithContext(
			t.Context(), method, target, strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequestWithContext(t.Context(), method, target, nil)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestHandleDashboard_RedirectsToHouseEditWhenNoHouse(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/", nil)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/house/edit", rec.Header().Get("Location"))
}

func TestHandleDashboard_RendersEmptyStateWithHouse(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Test House"}))

	rec := do(t, srv, http.MethodGet, "/", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Nothing needs attention")
}

func TestHandleHouseView_RedirectsToEditWhenNoHouse(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/house", nil)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/house/edit", rec.Header().Get("Location"))
}

func TestHouseForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"nickname":      {"My House"},
		"address_line1": {"123 Main St"},
		"city":          {"Springfield"},
		"state":         {"IL"},
		"postal_code":   {"62704"},
		"year_built":    {"1998"},
		"square_feet":   {"2100"},
		"bedrooms":      {"3"},
		"bathrooms":     {"2.5"},
	}
	rec := do(t, srv, http.MethodPost, "/house", form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/house", rec.Header().Get("Location"))

	house, err := srv.store.HouseProfile()
	require.NoError(t, err)
	require.Equal(t, "My House", house.Nickname)
	require.Equal(t, "Springfield", house.City)
	require.Equal(t, 1998, house.YearBuilt)
	require.Equal(t, 2100, house.SquareFeet)
	require.Equal(t, 3, house.Bedrooms)
	require.InDelta(t, 2.5, house.Bathrooms, 0.001)

	viewRec := do(t, srv, http.MethodGet, "/house", nil)
	require.Equal(t, http.StatusOK, viewRec.Code)
	require.Contains(t, viewRec.Body.String(), "My House")
}

func TestHouseForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Original"}))

	form := url.Values{"nickname": {"Renamed House"}}
	rec := do(t, srv, http.MethodPost, "/house", form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	house, err := srv.store.HouseProfile()
	require.NoError(t, err)
	require.Equal(t, "Renamed House", house.Nickname)
}

func TestHouseForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	form := url.Values{
		"nickname":   {"Bad Data House"},
		"year_built": {"not-a-number"},
	}
	rec := do(t, srv, http.MethodPost, "/house", form)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Year Built should be a whole number")

	_, err := srv.store.HouseProfile()
	require.Error(t, err, "invalid submission must not create a house profile")
}

func TestHandleHouseEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{
		Nickname:   "Prefill House",
		YearBuilt:  2005,
		SquareFeet: 1800,
	}))

	rec := do(t, srv, http.MethodGet, "/house/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill House"`)
	require.Contains(t, rec.Body.String(), `value="2005"`)
	require.Contains(t, rec.Body.String(), `value="1800"`)
}

func TestHandleDashboard_RendersOverdueAndUpcomingMaintenance(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Test House"}))

	cats, err := srv.store.MaintenanceCategories()
	require.NoError(t, err)
	require.NotEmpty(t, cats)
	categoryID := cats[0].ID

	overdueDue := time.Now().AddDate(0, 0, -30)
	require.NoError(t, srv.store.CreateMaintenance(&data.MaintenanceItem{
		Name: "Overdue Gutter Cleaning", CategoryID: categoryID, DueDate: &overdueDue,
	}))

	upcomingDue := time.Now().AddDate(0, 0, 10)
	require.NoError(t, srv.store.CreateMaintenance(&data.MaintenanceItem{
		Name: "Upcoming Filter Change", CategoryID: categoryID, DueDate: &upcomingDue,
	}))

	rec := do(t, srv, http.MethodGet, "/", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Overdue Gutter Cleaning")
	require.Contains(t, rec.Body.String(), "Upcoming Filter Change")
	require.NotContains(t, rec.Body.String(), "Nothing needs attention")
}

func TestHandleDashboard_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Test House"}))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, "/", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleHouseView_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Test House"}))
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, "/house", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleHouseEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, "/house/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHouseForm_SubmitStoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodPost, "/house", url.Values{"nickname": {"Doesn't matter"}})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHouseForm_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		form    url.Values
		wantErr string
	}{
		{
			name:    "invalid square feet",
			form:    url.Values{"square_feet": {"not-a-number"}},
			wantErr: "Square feet should be a whole number",
		},
		{
			name:    "invalid lot square feet",
			form:    url.Values{"lot_square_feet": {"not-a-number"}},
			wantErr: "Lot square feet should be a whole number",
		},
		{
			name:    "invalid bedrooms",
			form:    url.Values{"bedrooms": {"not-a-number"}},
			wantErr: "Bedrooms should be a whole number",
		},
		{
			name:    "invalid bathrooms",
			form:    url.Values{"bathrooms": {"not-a-number"}},
			wantErr: "Bathrooms should be a number like 2.5",
		},
		{
			name:    "invalid insurance renewal date",
			form:    url.Values{"insurance_renewal": {"not-a-date"}},
			wantErr: "Insurance Renewal should be YYYY-MM-DD",
		},
		{
			name:    "invalid property tax",
			form:    url.Values{"property_tax": {"not-money"}},
			wantErr: "Property Tax should look like 1250.00",
		},
		{
			name:    "invalid hoa fee",
			form:    url.Values{"hoa_fee": {"not-money"}},
			wantErr: "HOA Fee should look like 1250.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer(t)

			rec := do(t, srv, http.MethodPost, "/house", tt.form)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			require.Contains(t, rec.Body.String(), tt.wantErr)

			_, err := srv.store.HouseProfile()
			require.Error(t, err, "invalid submission must not create a house profile")
		})
	}
}

func TestServerRun_ServesAndShutsDownOnContextCancel(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.CreateHouseProfile(data.HouseProfile{Nickname: "Run Test House"}))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx, addr) }()

	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/house", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond, "server did not become ready")

	cancel()
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not shut down after context cancellation")
	}
}

func TestStaticAssets_Served(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/static/style.css", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "--accent")
}
