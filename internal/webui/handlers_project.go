// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/micasa-dev/micasa/internal/data"
)

const (
	projectNav  = "projects"
	projectsURL = "/projects"
)

// projectStatuses lists every valid Project.Status value, in the same
// order the TUI's statusOptions() presents them.
var projectStatuses = []string{
	data.ProjectStatusIdeating,
	data.ProjectStatusPlanned,
	data.ProjectStatusQuoted,
	data.ProjectStatusInProgress,
	data.ProjectStatusDelayed,
	data.ProjectStatusCompleted,
	data.ProjectStatusAbandoned,
}

type projectListPageData struct {
	pageData

	Projects       []projectRow
	IncludeDeleted bool
	Error          string
}

// projectRow pairs a Project with its display-formatted budget, since money
// formatting depends on Server.cur and templates should not format money
// themselves.
type projectRow struct {
	data.Project

	Budget string
}

type projectFormPageData struct {
	pageData

	Project      data.Project
	ProjectTypes []data.ProjectType
	Statuses     []string
	IsNew        bool
	Error        string
	StartDate    string
	EndDate      string
	Budget       string
	Actual       string
}

func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	projects, err := s.store.ListProjects(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load projects: %w", err))
		return
	}
	rows := make([]projectRow, len(projects))
	for i, p := range projects {
		rows[i] = projectRow{Project: p, Budget: s.cur.FormatOptionalCents(p.BudgetCents)}
	}
	s.render(w, http.StatusOK, "project_list.html", projectListPageData{
		pageData:       pageData{Title: "Projects", Nav: projectNav},
		Projects:       rows,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

func (s *Server) handleProjectNewForm(w http.ResponseWriter, _ *http.Request) {
	projectTypes, err := s.store.ProjectTypes()
	if err != nil {
		s.renderError(w, fmt.Errorf("load project types: %w", err))
		return
	}
	project := data.Project{Status: data.ProjectStatusIdeating}
	if len(projectTypes) > 0 {
		project.ProjectTypeID = projectTypes[0].ID
	}
	s.render(w, http.StatusOK, "project_form.html", projectFormPageData{
		pageData:     pageData{Title: "New Project", Nav: projectNav},
		Project:      project,
		ProjectTypes: projectTypes,
		Statuses:     projectStatuses,
		IsNew:        true,
	})
}

func (s *Server) handleProjectEditForm(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.GetProject(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load project: %w", err))
		return
	}
	projectTypes, err := s.store.ProjectTypes()
	if err != nil {
		s.renderError(w, fmt.Errorf("load project types: %w", err))
		return
	}
	s.render(w, http.StatusOK, "project_form.html", projectFormPageData{
		pageData:     pageData{Title: "Edit Project", Nav: projectNav},
		Project:      project,
		ProjectTypes: projectTypes,
		Statuses:     projectStatuses,
		StartDate:    formatOptionalDate(project.StartDate),
		EndDate:      formatOptionalDate(project.EndDate),
		Budget:       s.cur.FormatOptionalCents(project.BudgetCents),
		Actual:       s.cur.FormatOptionalCents(project.ActualCents),
	})
}

// parseProjectForm mirrors internal/app/forms.go's parseProjectFormData:
// only Title is required.
func (s *Server) parseProjectForm(r *http.Request) (data.Project, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return data.Project{}, errors.New("title is required")
	}
	budget, err := s.cur.ParseOptionalCents(r.FormValue("budget"))
	if err != nil {
		return data.Project{}, data.FieldError("Budget", err)
	}
	actual, err := s.cur.ParseOptionalCents(r.FormValue("actual"))
	if err != nil {
		return data.Project{}, data.FieldError("Actual", err)
	}
	startDate, err := data.ParseOptionalDate(r.FormValue("start_date"))
	if err != nil {
		return data.Project{}, data.FieldError("Start Date", err)
	}
	endDate, err := data.ParseOptionalDate(r.FormValue("end_date"))
	if err != nil {
		return data.Project{}, data.FieldError("End Date", err)
	}
	return data.Project{
		Title:         title,
		ProjectTypeID: r.FormValue("project_type_id"),
		Status:        r.FormValue("status"),
		Description:   strings.TrimSpace(r.FormValue("description")),
		StartDate:     startDate,
		EndDate:       endDate,
		BudgetCents:   budget,
		ActualCents:   actual,
	}, nil
}

func (s *Server) projectFormErrorPage(
	title string, project data.Project, r *http.Request, err error,
) (projectFormPageData, error) {
	projectTypes, loadErr := s.store.ProjectTypes()
	if loadErr != nil {
		return projectFormPageData{}, loadErr
	}
	return projectFormPageData{
		pageData:     pageData{Title: title, Nav: projectNav},
		Project:      project,
		ProjectTypes: projectTypes,
		Statuses:     projectStatuses,
		IsNew:        title == "New Project",
		Error:        err.Error(),
		StartDate:    r.FormValue("start_date"),
		EndDate:      r.FormValue("end_date"),
		Budget:       r.FormValue("budget"),
		Actual:       r.FormValue("actual"),
	}, nil
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	project, formErr := s.parseProjectForm(r)
	if formErr != nil {
		page, err := s.projectFormErrorPage("New Project", project, r, formErr)
		if err != nil {
			s.renderError(w, fmt.Errorf("load project types: %w", err))
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "project_form.html", page)
		return
	}
	if err := s.store.CreateProject(&project); err != nil {
		page, pageErr := s.projectFormErrorPage("New Project", project, r, err)
		if pageErr != nil {
			s.renderError(w, fmt.Errorf("load project types: %w", pageErr))
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "project_form.html", page)
		return
	}
	http.Redirect(w, r, projectsURL, http.StatusSeeOther)
}

func (s *Server) handleProjectUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	project, formErr := s.parseProjectForm(r)
	if formErr != nil {
		project.ID = id
		page, err := s.projectFormErrorPage("Edit Project", project, r, formErr)
		if err != nil {
			s.renderError(w, fmt.Errorf("load project types: %w", err))
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "project_form.html", page)
		return
	}
	project.ID = id
	if err := s.store.UpdateProject(project); err != nil {
		page, pageErr := s.projectFormErrorPage("Edit Project", project, r, err)
		if pageErr != nil {
			s.renderError(w, fmt.Errorf("load project types: %w", pageErr))
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "project_form.html", page)
		return
	}
	http.Redirect(w, r, projectsURL, http.StatusSeeOther)
}

func (s *Server) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProject(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, projectsURL, err)
		return
	}
	http.Redirect(w, r, projectsURL, http.StatusSeeOther)
}

func (s *Server) handleProjectRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreProject(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, projectsURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, projectsURL+"?deleted=1", http.StatusSeeOther)
}
