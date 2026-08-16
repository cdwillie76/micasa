// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/micasa-dev/micasa/internal/data"
)

const (
	maintenanceNav = "maintenance"
	maintenanceURL = "/maintenance"
	schedNone      = "none"
	schedInterval  = "interval"
	schedDueDate   = "due_date"
)

var maintenanceSeasons = []string{
	"",
	data.SeasonSpring,
	data.SeasonSummer,
	data.SeasonFall,
	data.SeasonWinter,
}

type maintenanceListPageData struct {
	pageData

	Items          []data.MaintenanceItem
	IncludeDeleted bool
	Error          string
}

type maintenanceFormPageData struct {
	pageData

	Item              data.MaintenanceItem
	Categories        []data.MaintenanceCategory
	Appliances        []data.Appliance
	Seasons           []string
	IsNew             bool
	Error             string
	LastServiced      string
	ScheduleType      string
	IntervalMonths    string
	DueDate           string
	Cost              string
	SelectedAppliance string
}

func (s *Server) handleMaintenanceList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	items, err := s.store.ListMaintenance(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load maintenance items: %w", err))
		return
	}
	s.render(w, http.StatusOK, "maintenance_list.html", maintenanceListPageData{
		pageData:       pageData{Title: "Maintenance", Nav: maintenanceNav},
		Items:          items,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

// maintenanceFormLookups loads the category/appliance option lists shared by
// the new, edit, and error-redisplay maintenance form pages.
func (s *Server) maintenanceFormLookups() ([]data.MaintenanceCategory, []data.Appliance, error) {
	categories, err := s.store.MaintenanceCategories()
	if err != nil {
		return nil, nil, fmt.Errorf("load maintenance categories: %w", err)
	}
	appliances, err := s.store.ListAppliances(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load appliances: %w", err)
	}
	return categories, appliances, nil
}

func (s *Server) handleMaintenanceNewForm(w http.ResponseWriter, _ *http.Request) {
	categories, appliances, err := s.maintenanceFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	item := data.MaintenanceItem{}
	if len(categories) > 0 {
		item.CategoryID = categories[0].ID
	}
	s.render(w, http.StatusOK, "maintenance_form.html", maintenanceFormPageData{
		pageData:     pageData{Title: "New Maintenance Item", Nav: maintenanceNav},
		Item:         item,
		Categories:   categories,
		Appliances:   appliances,
		Seasons:      maintenanceSeasons,
		IsNew:        true,
		ScheduleType: schedNone,
	})
}

func (s *Server) handleMaintenanceEditForm(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetMaintenance(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load maintenance item: %w", err))
		return
	}
	categories, appliances, err := s.maintenanceFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	sched := schedNone
	switch {
	case item.DueDate != nil:
		sched = schedDueDate
	case item.IntervalMonths > 0:
		sched = schedInterval
	}
	s.render(w, http.StatusOK, "maintenance_form.html", maintenanceFormPageData{
		pageData:          pageData{Title: "Edit Maintenance Item", Nav: maintenanceNav},
		Item:              item,
		Categories:        categories,
		Appliances:        appliances,
		Seasons:           maintenanceSeasons,
		LastServiced:      formatOptionalDate(item.LastServicedAt),
		ScheduleType:      sched,
		IntervalMonths:    formatOptionalInt(item.IntervalMonths),
		DueDate:           formatOptionalDate(item.DueDate),
		Cost:              s.cur.FormatOptionalCents(item.CostCents),
		SelectedAppliance: derefString(item.ApplianceID),
	})
}

// parseMaintenanceForm mirrors internal/app/forms.go's parseMaintenanceFormData:
// the schedule_type radio picks which of interval/due-date is parsed, exactly
// as the TUI's schedule-type selector enforces mutual exclusion at the UI level.
func (s *Server) parseMaintenanceForm(r *http.Request) (data.MaintenanceItem, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return data.MaintenanceItem{}, errors.New("name is required")
	}
	lastServiced, err := data.ParseOptionalDate(r.FormValue("last_serviced"))
	if err != nil {
		return data.MaintenanceItem{}, data.FieldError("Last Serviced", err)
	}

	var interval int
	var dueDate *time.Time
	switch r.FormValue("schedule_type") {
	case schedInterval:
		interval, err = data.ParseIntervalMonths(r.FormValue("interval_months"))
		if err != nil {
			return data.MaintenanceItem{}, data.FieldError("Interval", err)
		}
	case schedDueDate:
		dueDate, err = data.ParseOptionalDate(r.FormValue("due_date"))
		if err != nil {
			return data.MaintenanceItem{}, data.FieldError("Due Date", err)
		}
	}

	cost, err := s.cur.ParseOptionalCents(r.FormValue("cost"))
	if err != nil {
		return data.MaintenanceItem{}, data.FieldError("Cost", err)
	}
	var applianceID *string
	if v := r.FormValue("appliance_id"); v != "" {
		applianceID = &v
	}
	return data.MaintenanceItem{
		Name:           name,
		CategoryID:     r.FormValue("category_id"),
		ApplianceID:    applianceID,
		Season:         r.FormValue("season"),
		LastServicedAt: lastServiced,
		IntervalMonths: interval,
		DueDate:        dueDate,
		ManualURL:      strings.TrimSpace(r.FormValue("manual_url")),
		CostCents:      cost,
		Notes:          strings.TrimSpace(r.FormValue("notes")),
	}, nil
}

func (s *Server) maintenanceFormErrorPage(
	title string, item data.MaintenanceItem, r *http.Request, err error,
) (maintenanceFormPageData, error) {
	categories, appliances, lookupErr := s.maintenanceFormLookups()
	if lookupErr != nil {
		return maintenanceFormPageData{}, lookupErr
	}
	return maintenanceFormPageData{
		pageData:          pageData{Title: title, Nav: maintenanceNav},
		Item:              item,
		Categories:        categories,
		Appliances:        appliances,
		Seasons:           maintenanceSeasons,
		IsNew:             title == "New Maintenance Item",
		Error:             err.Error(),
		LastServiced:      r.FormValue("last_serviced"),
		ScheduleType:      r.FormValue("schedule_type"),
		IntervalMonths:    r.FormValue("interval_months"),
		DueDate:           r.FormValue("due_date"),
		Cost:              r.FormValue("cost"),
		SelectedAppliance: derefString(item.ApplianceID),
	}, nil
}

func (s *Server) handleMaintenanceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	item, formErr := s.parseMaintenanceForm(r)
	if formErr != nil {
		page, err := s.maintenanceFormErrorPage("New Maintenance Item", item, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "maintenance_form.html", page)
		return
	}
	if err := s.store.CreateMaintenance(&item); err != nil {
		page, pageErr := s.maintenanceFormErrorPage("New Maintenance Item", item, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "maintenance_form.html", page)
		return
	}
	http.Redirect(w, r, maintenanceURL, http.StatusSeeOther)
}

func (s *Server) handleMaintenanceUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	item, formErr := s.parseMaintenanceForm(r)
	if formErr != nil {
		item.ID = id
		page, err := s.maintenanceFormErrorPage("Edit Maintenance Item", item, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "maintenance_form.html", page)
		return
	}
	item.ID = id
	if err := s.store.UpdateMaintenance(item); err != nil {
		page, pageErr := s.maintenanceFormErrorPage("Edit Maintenance Item", item, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "maintenance_form.html", page)
		return
	}
	http.Redirect(w, r, maintenanceURL, http.StatusSeeOther)
}

func (s *Server) handleMaintenanceDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMaintenance(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, maintenanceURL, err)
		return
	}
	http.Redirect(w, r, maintenanceURL, http.StatusSeeOther)
}

func (s *Server) handleMaintenanceRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreMaintenance(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, maintenanceURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, maintenanceURL+"?deleted=1", http.StatusSeeOther)
}
