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
	vendorNav  = "vendors"
	vendorsURL = "/vendors"
)

type vendorListPageData struct {
	pageData

	Vendors        []data.Vendor
	IncludeDeleted bool
	Error          string
}

type vendorFormPageData struct {
	pageData

	Vendor data.Vendor
	IsNew  bool
	Error  string
}

func (s *Server) handleVendorList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	vendors, err := s.store.ListVendors(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load vendors: %w", err))
		return
	}
	s.render(w, http.StatusOK, "vendor_list.html", vendorListPageData{
		pageData:       pageData{Title: "Vendors", Nav: vendorNav, Wide: true},
		Vendors:        vendors,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

func (s *Server) handleVendorNewForm(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusOK, "vendor_form.html", vendorFormPageData{
		pageData: pageData{Title: "New Vendor", Nav: vendorNav},
		IsNew:    true,
	})
}

func (s *Server) handleVendorEditForm(w http.ResponseWriter, r *http.Request) {
	vendor, err := s.store.GetVendor(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load vendor: %w", err))
		return
	}
	s.render(w, http.StatusOK, "vendor_form.html", vendorFormPageData{
		pageData: pageData{Title: "Edit Vendor", Nav: vendorNav},
		Vendor:   vendor,
	})
}

// parseVendorForm mirrors internal/app/forms.go's parseVendorFormData: only
// Name is required, every other field is trimmed free text.
func (s *Server) parseVendorForm(r *http.Request) (data.Vendor, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return data.Vendor{}, errors.New("name is required")
	}
	return data.Vendor{
		Name:        name,
		ContactName: strings.TrimSpace(r.FormValue("contact_name")),
		Email:       strings.TrimSpace(r.FormValue("email")),
		Phone:       strings.TrimSpace(r.FormValue("phone")),
		Website:     strings.TrimSpace(r.FormValue("website")),
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}, nil
}

func (s *Server) handleVendorCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	vendor, formErr := s.parseVendorForm(r)
	if formErr != nil {
		s.render(w, http.StatusUnprocessableEntity, "vendor_form.html", vendorFormPageData{
			pageData: pageData{Title: "New Vendor", Nav: vendorNav},
			Vendor:   vendor,
			IsNew:    true,
			Error:    formErr.Error(),
		})
		return
	}
	if err := s.store.CreateVendor(&vendor); err != nil {
		s.render(w, http.StatusUnprocessableEntity, "vendor_form.html", vendorFormPageData{
			pageData: pageData{Title: "New Vendor", Nav: vendorNav},
			Vendor:   vendor,
			IsNew:    true,
			Error:    err.Error(),
		})
		return
	}
	http.Redirect(w, r, vendorsURL, http.StatusSeeOther)
}

func (s *Server) handleVendorUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	vendor, formErr := s.parseVendorForm(r)
	if formErr != nil {
		vendor.ID = id
		s.render(w, http.StatusUnprocessableEntity, "vendor_form.html", vendorFormPageData{
			pageData: pageData{Title: "Edit Vendor", Nav: vendorNav},
			Vendor:   vendor,
			Error:    formErr.Error(),
		})
		return
	}
	vendor.ID = id
	if err := s.store.UpdateVendor(vendor); err != nil {
		s.render(w, http.StatusUnprocessableEntity, "vendor_form.html", vendorFormPageData{
			pageData: pageData{Title: "Edit Vendor", Nav: vendorNav},
			Vendor:   vendor,
			Error:    err.Error(),
		})
		return
	}
	http.Redirect(w, r, vendorsURL, http.StatusSeeOther)
}

func (s *Server) handleVendorDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteVendor(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, vendorsURL, err)
		return
	}
	http.Redirect(w, r, vendorsURL, http.StatusSeeOther)
}

func (s *Server) handleVendorRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreVendor(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, vendorsURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, vendorsURL+"?deleted=1", http.StatusSeeOther)
}
