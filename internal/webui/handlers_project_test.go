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

func TestProjectList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, projectsURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No projects yet.")
}

func TestProjectNewForm_ListsProjectTypesAndStatuses(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, projectsURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "<select name=\"project_type_id\">")
	require.Contains(t, rec.Body.String(), data.ProjectStatusIdeating)
	require.Contains(t, rec.Body.String(), data.ProjectStatusAbandoned)
}

func TestProjectForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)
	projectTypes, err := srv.store.ProjectTypes()
	require.NoError(t, err)
	require.NotEmpty(t, projectTypes)

	form := url.Values{
		"title":           {"Kitchen Remodel"},
		"project_type_id": {projectTypes[0].ID},
		"status":          {data.ProjectStatusPlanned},
		"budget":          {"5000.00"},
	}
	rec := do(t, srv, http.MethodPost, projectsURL, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, projectsURL, rec.Header().Get("Location"))

	projects, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "Kitchen Remodel", projects[0].Title)
	require.Equal(t, data.ProjectStatusPlanned, projects[0].Status)
	require.Equal(t, int64(500000), *projects[0].BudgetCents)
}

func TestProjectForm_UpdateFlow(t *testing.T) {
	srv := newTestServer(t)
	projectTypes, err := srv.store.ProjectTypes()
	require.NoError(t, err)
	project := data.Project{Title: "Original Title", ProjectTypeID: projectTypes[0].ID}
	require.NoError(t, srv.store.CreateProject(&project))

	form := url.Values{
		"title":           {"Updated Title"},
		"project_type_id": {projectTypes[0].ID},
		"status":          {data.ProjectStatusCompleted},
	}
	rec := do(t, srv, http.MethodPost, projectsURL+"/"+project.ID, form)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	updated, err := srv.store.GetProject(project.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", updated.Title)
	require.Equal(t, data.ProjectStatusCompleted, updated.Status)
}

func TestProjectForm_ValidationErrorRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, projectsURL, url.Values{"title": {"   "}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "title is required")

	projects, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestProjectForm_InvalidBudgetRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodPost, projectsURL, url.Values{
		"title": {"Bad Budget Project"}, "budget": {"not-money"},
	})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "Budget should look like")

	projects, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestProjectDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	project := data.Project{Title: "Deletable Project", ProjectTypeID: firstProjectTypeID(t, srv)}
	require.NoError(t, srv.store.CreateProject(&project))

	deleteRec := do(t, srv, http.MethodPost, projectsURL+"/"+project.ID+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, projectsURL+"/"+project.ID+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestProjectDelete_BlockedByDependentQuoteRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	project := data.Project{Title: "Project With Quote", ProjectTypeID: firstProjectTypeID(t, srv)}
	require.NoError(t, srv.store.CreateProject(&project))
	vendor := data.Vendor{Name: "Quote Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	require.NoError(t, srv.store.CreateQuote(&data.Quote{
		ProjectID: project.ID, VendorID: vendor.ID,
	}, vendor))

	rec := do(t, srv, http.MethodPost, projectsURL+"/"+project.ID+"/delete", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")

	projects, err := srv.store.ListProjects(false)
	require.NoError(t, err)
	require.Len(t, projects, 1, "delete must be blocked while a dependent quote exists")
}

func TestProjectEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	project := data.Project{
		Title: "Prefill Project", ProjectTypeID: firstProjectTypeID(t, srv),
		Status: data.ProjectStatusPlanned,
	}
	require.NoError(t, srv.store.CreateProject(&project))

	rec := do(t, srv, http.MethodGet, projectsURL+"/"+project.ID+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill Project"`)
}

func TestProjectEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, projectsURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProjectList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, projectsURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
