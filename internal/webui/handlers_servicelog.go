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
	serviceLogNav = "service-logs"
	serviceLogURL = "/service-logs"
)

type serviceLogListPageData struct {
	pageData

	Entries        []data.ServiceLogEntry
	IncludeDeleted bool
	Error          string
}

type serviceLogFormPageData struct {
	pageData

	Entry             data.ServiceLogEntry
	MaintenanceItems  []data.MaintenanceItem
	Vendors           []data.Vendor
	IsNew             bool
	Error             string
	ServicedAt        string
	Cost              string
	SelectedVendor    string
	SelectedMaintItem string
}

func (s *Server) handleServiceLogList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	entries, err := s.store.ListAllServiceLogEntries(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load service logs: %w", err))
		return
	}
	s.render(w, http.StatusOK, "servicelog_list.html", serviceLogListPageData{
		pageData:       pageData{Title: "Service Logs", Nav: serviceLogNav},
		Entries:        entries,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

// serviceLogFormLookups loads the maintenance-item/vendor option lists
// shared by the new, edit, and error-redisplay service log form pages.
func (s *Server) serviceLogFormLookups() ([]data.MaintenanceItem, []data.Vendor, error) {
	items, err := s.store.ListMaintenance(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load maintenance items: %w", err)
	}
	vendors, err := s.store.ListVendors(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load vendors: %w", err)
	}
	return items, vendors, nil
}

func (s *Server) handleServiceLogNewForm(w http.ResponseWriter, _ *http.Request) {
	items, vendors, err := s.serviceLogFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	entry := data.ServiceLogEntry{}
	if len(items) > 0 {
		entry.MaintenanceItemID = items[0].ID
	}
	s.render(w, http.StatusOK, "servicelog_form.html", serviceLogFormPageData{
		pageData:         pageData{Title: "New Service Log Entry", Nav: serviceLogNav},
		Entry:            entry,
		MaintenanceItems: items,
		Vendors:          vendors,
		IsNew:            true,
	})
}

func (s *Server) handleServiceLogEditForm(w http.ResponseWriter, r *http.Request) {
	entry, err := s.store.GetServiceLog(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load service log entry: %w", err))
		return
	}
	items, vendors, err := s.serviceLogFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "servicelog_form.html", serviceLogFormPageData{
		pageData:          pageData{Title: "Edit Service Log Entry", Nav: serviceLogNav},
		Entry:             entry,
		MaintenanceItems:  items,
		Vendors:           vendors,
		ServicedAt:        entry.ServicedAt.Format("2006-01-02"),
		Cost:              s.cur.FormatOptionalCents(entry.CostCents),
		SelectedVendor:    derefString(entry.VendorID),
		SelectedMaintItem: entry.MaintenanceItemID,
	})
}

// parseServiceLogForm mirrors internal/app/forms.go's parseServiceLogFormData:
// MaintenanceItemID and ServicedAt are required; Vendor is optional.
func (s *Server) parseServiceLogForm(r *http.Request) (data.ServiceLogEntry, data.Vendor, error) {
	maintenanceItemID := r.FormValue("maintenance_item_id")
	if maintenanceItemID == "" {
		return data.ServiceLogEntry{}, data.Vendor{}, errors.New("maintenance item is required")
	}
	servicedAt, err := data.ParseRequiredDate(r.FormValue("serviced_at"))
	if err != nil {
		return data.ServiceLogEntry{}, data.Vendor{}, data.FieldError("Serviced At", err)
	}
	cost, err := s.cur.ParseOptionalCents(r.FormValue("cost"))
	if err != nil {
		return data.ServiceLogEntry{}, data.Vendor{}, data.FieldError("Cost", err)
	}
	entry := data.ServiceLogEntry{
		MaintenanceItemID: maintenanceItemID,
		ServicedAt:        servicedAt,
		CostCents:         cost,
		Notes:             strings.TrimSpace(r.FormValue("notes")),
	}
	var vendor data.Vendor
	if vendorID := r.FormValue("vendor_id"); vendorID != "" {
		found, err := s.store.GetVendor(vendorID)
		if err != nil {
			return data.ServiceLogEntry{}, data.Vendor{}, errors.New("selected vendor not found")
		}
		vendor = found
	}
	return entry, vendor, nil
}

func (s *Server) serviceLogFormErrorPage(
	title string, entry data.ServiceLogEntry, r *http.Request, err error,
) (serviceLogFormPageData, error) {
	items, vendors, lookupErr := s.serviceLogFormLookups()
	if lookupErr != nil {
		return serviceLogFormPageData{}, lookupErr
	}
	return serviceLogFormPageData{
		pageData:          pageData{Title: title, Nav: serviceLogNav},
		Entry:             entry,
		MaintenanceItems:  items,
		Vendors:           vendors,
		IsNew:             title == "New Service Log Entry",
		Error:             err.Error(),
		ServicedAt:        r.FormValue("serviced_at"),
		Cost:              r.FormValue("cost"),
		SelectedVendor:    r.FormValue("vendor_id"),
		SelectedMaintItem: r.FormValue("maintenance_item_id"),
	}, nil
}

func (s *Server) handleServiceLogCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	entry, vendor, formErr := s.parseServiceLogForm(r)
	if formErr != nil {
		page, err := s.serviceLogFormErrorPage("New Service Log Entry", entry, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "servicelog_form.html", page)
		return
	}
	if err := s.store.CreateServiceLog(&entry, vendor); err != nil {
		page, pageErr := s.serviceLogFormErrorPage("New Service Log Entry", entry, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "servicelog_form.html", page)
		return
	}
	http.Redirect(w, r, serviceLogURL, http.StatusSeeOther)
}

func (s *Server) handleServiceLogUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	entry, vendor, formErr := s.parseServiceLogForm(r)
	if formErr != nil {
		entry.ID = id
		page, err := s.serviceLogFormErrorPage("Edit Service Log Entry", entry, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "servicelog_form.html", page)
		return
	}
	entry.ID = id
	if err := s.store.UpdateServiceLog(entry, vendor); err != nil {
		page, pageErr := s.serviceLogFormErrorPage("Edit Service Log Entry", entry, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "servicelog_form.html", page)
		return
	}
	http.Redirect(w, r, serviceLogURL, http.StatusSeeOther)
}

func (s *Server) handleServiceLogDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteServiceLog(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, serviceLogURL, err)
		return
	}
	http.Redirect(w, r, serviceLogURL, http.StatusSeeOther)
}

func (s *Server) handleServiceLogRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreServiceLog(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, serviceLogURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, serviceLogURL+"?deleted=1", http.StatusSeeOther)
}
