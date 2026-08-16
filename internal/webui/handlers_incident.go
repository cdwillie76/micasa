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
	incidentNav  = "incidents"
	incidentsURL = "/incidents"
)

var incidentStatuses = []string{
	data.IncidentStatusOpen,
	data.IncidentStatusInProgress,
	data.IncidentStatusResolved,
}

var incidentSeverities = []string{
	data.IncidentSeverityUrgent,
	data.IncidentSeveritySoon,
	data.IncidentSeverityWhenever,
}

type incidentListPageData struct {
	pageData

	Incidents      []data.Incident
	IncludeDeleted bool
	Error          string
}

type incidentFormPageData struct {
	pageData

	Incident          data.Incident
	Appliances        []data.Appliance
	Vendors           []data.Vendor
	Statuses          []string
	Severities        []string
	IsNew             bool
	Error             string
	DateNoticed       string
	DateResolved      string
	Cost              string
	SelectedAppliance string
	SelectedVendor    string
}

func (s *Server) handleIncidentList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	incidents, err := s.store.ListIncidents(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load incidents: %w", err))
		return
	}
	s.render(w, http.StatusOK, "incident_list.html", incidentListPageData{
		pageData:       pageData{Title: "Incidents", Nav: incidentNav},
		Incidents:      incidents,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

// incidentFormLookups loads the appliance/vendor option lists shared by the
// new, edit, and error-redisplay incident form pages.
func (s *Server) incidentFormLookups() ([]data.Appliance, []data.Vendor, error) {
	appliances, err := s.store.ListAppliances(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load appliances: %w", err)
	}
	vendors, err := s.store.ListVendors(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load vendors: %w", err)
	}
	return appliances, vendors, nil
}

func (s *Server) handleIncidentNewForm(w http.ResponseWriter, _ *http.Request) {
	appliances, vendors, err := s.incidentFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "incident_form.html", incidentFormPageData{
		pageData: pageData{Title: "New Incident", Nav: incidentNav},
		Incident: data.Incident{
			Status:   data.IncidentStatusOpen,
			Severity: data.IncidentSeveritySoon,
		},
		Appliances: appliances,
		Vendors:    vendors,
		Statuses:   incidentStatuses,
		Severities: incidentSeverities,
		IsNew:      true,
	})
}

func (s *Server) handleIncidentEditForm(w http.ResponseWriter, r *http.Request) {
	incident, err := s.store.GetIncident(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load incident: %w", err))
		return
	}
	appliances, vendors, err := s.incidentFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "incident_form.html", incidentFormPageData{
		pageData:          pageData{Title: "Edit Incident", Nav: incidentNav},
		Incident:          incident,
		Appliances:        appliances,
		Vendors:           vendors,
		Statuses:          incidentStatuses,
		Severities:        incidentSeverities,
		DateNoticed:       incident.DateNoticed.Format("2006-01-02"),
		DateResolved:      formatOptionalDate(incident.DateResolved),
		Cost:              s.cur.FormatOptionalCents(incident.CostCents),
		SelectedAppliance: derefString(incident.ApplianceID),
		SelectedVendor:    derefString(incident.VendorID),
	})
}

// parseIncidentForm mirrors internal/app/forms.go's parseIncidentFormData:
// Title and Date Noticed are required; Appliance/Vendor are optional FKs.
func (s *Server) parseIncidentForm(r *http.Request) (data.Incident, error) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		return data.Incident{}, errors.New("title is required")
	}
	noticed, err := data.ParseRequiredDate(r.FormValue("date_noticed"))
	if err != nil {
		return data.Incident{}, data.FieldError("Date Noticed", err)
	}
	resolved, err := data.ParseOptionalDate(r.FormValue("date_resolved"))
	if err != nil {
		return data.Incident{}, data.FieldError("Date Resolved", err)
	}
	cost, err := s.cur.ParseOptionalCents(r.FormValue("cost"))
	if err != nil {
		return data.Incident{}, data.FieldError("Cost", err)
	}
	var applianceID *string
	if v := r.FormValue("appliance_id"); v != "" {
		applianceID = &v
	}
	var vendorID *string
	if v := r.FormValue("vendor_id"); v != "" {
		vendorID = &v
	}
	// The new-incident form has no status field (matches the TUI, which
	// only exposes status on edit) -- default it to "open".
	status := r.FormValue("status")
	if status == "" {
		status = data.IncidentStatusOpen
	}
	return data.Incident{
		Title:        title,
		Description:  strings.TrimSpace(r.FormValue("description")),
		Status:       status,
		Severity:     r.FormValue("severity"),
		DateNoticed:  noticed,
		DateResolved: resolved,
		Location:     strings.TrimSpace(r.FormValue("location")),
		CostCents:    cost,
		ApplianceID:  applianceID,
		VendorID:     vendorID,
		Notes:        strings.TrimSpace(r.FormValue("notes")),
	}, nil
}

func (s *Server) incidentFormErrorPage(
	title string, incident data.Incident, r *http.Request, err error,
) (incidentFormPageData, error) {
	appliances, vendors, lookupErr := s.incidentFormLookups()
	if lookupErr != nil {
		return incidentFormPageData{}, lookupErr
	}
	return incidentFormPageData{
		pageData:          pageData{Title: title, Nav: incidentNav},
		Incident:          incident,
		Appliances:        appliances,
		Vendors:           vendors,
		Statuses:          incidentStatuses,
		Severities:        incidentSeverities,
		IsNew:             title == "New Incident",
		Error:             err.Error(),
		DateNoticed:       r.FormValue("date_noticed"),
		DateResolved:      r.FormValue("date_resolved"),
		Cost:              r.FormValue("cost"),
		SelectedAppliance: derefString(incident.ApplianceID),
		SelectedVendor:    derefString(incident.VendorID),
	}, nil
}

// derefString returns "" for a nil *string, otherwise the pointed-to value.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) handleIncidentCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	incident, formErr := s.parseIncidentForm(r)
	if formErr != nil {
		page, err := s.incidentFormErrorPage("New Incident", incident, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "incident_form.html", page)
		return
	}
	if err := s.store.CreateIncident(&incident); err != nil {
		page, pageErr := s.incidentFormErrorPage("New Incident", incident, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "incident_form.html", page)
		return
	}
	http.Redirect(w, r, incidentsURL, http.StatusSeeOther)
}

func (s *Server) handleIncidentUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	incident, formErr := s.parseIncidentForm(r)
	if formErr != nil {
		incident.ID = id
		page, err := s.incidentFormErrorPage("Edit Incident", incident, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "incident_form.html", page)
		return
	}
	incident.ID = id
	if err := s.store.UpdateIncident(incident); err != nil {
		page, pageErr := s.incidentFormErrorPage("Edit Incident", incident, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "incident_form.html", page)
		return
	}
	http.Redirect(w, r, incidentsURL, http.StatusSeeOther)
}

func (s *Server) handleIncidentDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteIncident(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, incidentsURL, err)
		return
	}
	http.Redirect(w, r, incidentsURL, http.StatusSeeOther)
}

func (s *Server) handleIncidentRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreIncident(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, incidentsURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, incidentsURL+"?deleted=1", http.StatusSeeOther)
}
