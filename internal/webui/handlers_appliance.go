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
	applianceNav  = "appliances"
	appliancesURL = "/appliances"
)

type applianceListPageData struct {
	pageData

	Appliances     []data.Appliance
	IncludeDeleted bool
	Error          string
}

type applianceFormPageData struct {
	pageData

	Appliance      data.Appliance
	IsNew          bool
	Error          string
	PurchaseDate   string
	WarrantyExpiry string
	Cost           string
}

func (s *Server) handleApplianceList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	appliances, err := s.store.ListAppliances(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load appliances: %w", err))
		return
	}
	s.render(w, http.StatusOK, "appliance_list.html", applianceListPageData{
		pageData:       pageData{Title: "Appliances", Nav: applianceNav},
		Appliances:     appliances,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

func (s *Server) handleApplianceNewForm(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusOK, "appliance_form.html", applianceFormPageData{
		pageData: pageData{Title: "New Appliance", Nav: applianceNav},
		IsNew:    true,
	})
}

func (s *Server) handleApplianceEditForm(w http.ResponseWriter, r *http.Request) {
	appliance, err := s.store.GetAppliance(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load appliance: %w", err))
		return
	}
	s.render(w, http.StatusOK, "appliance_form.html", applianceFormPageData{
		pageData:       pageData{Title: "Edit Appliance", Nav: applianceNav},
		Appliance:      appliance,
		PurchaseDate:   formatOptionalDate(appliance.PurchaseDate),
		WarrantyExpiry: formatOptionalDate(appliance.WarrantyExpiry),
		Cost:           s.cur.FormatOptionalCents(appliance.CostCents),
	})
}

// parseApplianceForm mirrors internal/app/forms.go's parseApplianceFormData:
// only Name is required.
func (s *Server) parseApplianceForm(r *http.Request) (data.Appliance, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return data.Appliance{}, errors.New("name is required")
	}
	purchaseDate, err := data.ParseOptionalDate(r.FormValue("purchase_date"))
	if err != nil {
		return data.Appliance{}, data.FieldError("Purchase Date", err)
	}
	warrantyExpiry, err := data.ParseOptionalDate(r.FormValue("warranty_expiry"))
	if err != nil {
		return data.Appliance{}, data.FieldError("Warranty Expiry", err)
	}
	cost, err := s.cur.ParseOptionalCents(r.FormValue("cost"))
	if err != nil {
		return data.Appliance{}, data.FieldError("Cost", err)
	}
	return data.Appliance{
		Name:           name,
		Brand:          strings.TrimSpace(r.FormValue("brand")),
		ModelNumber:    strings.TrimSpace(r.FormValue("model_number")),
		SerialNumber:   strings.TrimSpace(r.FormValue("serial_number")),
		PurchaseDate:   purchaseDate,
		WarrantyExpiry: warrantyExpiry,
		Location:       strings.TrimSpace(r.FormValue("location")),
		CostCents:      cost,
		Notes:          strings.TrimSpace(r.FormValue("notes")),
	}, nil
}

func (s *Server) applianceFormErrorPage(
	title string,
	appliance data.Appliance,
	r *http.Request,
	err error,
) applianceFormPageData {
	return applianceFormPageData{
		pageData:       pageData{Title: title, Nav: applianceNav},
		Appliance:      appliance,
		IsNew:          title == "New Appliance",
		Error:          err.Error(),
		PurchaseDate:   r.FormValue("purchase_date"),
		WarrantyExpiry: r.FormValue("warranty_expiry"),
		Cost:           r.FormValue("cost"),
	}
}

func (s *Server) handleApplianceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	appliance, formErr := s.parseApplianceForm(r)
	if formErr != nil {
		s.render(w, http.StatusUnprocessableEntity, "appliance_form.html",
			s.applianceFormErrorPage("New Appliance", appliance, r, formErr))
		return
	}
	if err := s.store.CreateAppliance(&appliance); err != nil {
		s.render(w, http.StatusUnprocessableEntity, "appliance_form.html",
			s.applianceFormErrorPage("New Appliance", appliance, r, err))
		return
	}
	http.Redirect(w, r, appliancesURL, http.StatusSeeOther)
}

func (s *Server) handleApplianceUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	appliance, formErr := s.parseApplianceForm(r)
	if formErr != nil {
		appliance.ID = id
		s.render(w, http.StatusUnprocessableEntity, "appliance_form.html",
			s.applianceFormErrorPage("Edit Appliance", appliance, r, formErr))
		return
	}
	appliance.ID = id
	if err := s.store.UpdateAppliance(appliance); err != nil {
		s.render(w, http.StatusUnprocessableEntity, "appliance_form.html",
			s.applianceFormErrorPage("Edit Appliance", appliance, r, err))
		return
	}
	http.Redirect(w, r, appliancesURL, http.StatusSeeOther)
}

func (s *Server) handleApplianceDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAppliance(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, appliancesURL, err)
		return
	}
	http.Redirect(w, r, appliancesURL, http.StatusSeeOther)
}

func (s *Server) handleApplianceRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreAppliance(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, appliancesURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, appliancesURL+"?deleted=1", http.StatusSeeOther)
}
