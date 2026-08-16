// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"errors"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/micasa-dev/micasa/internal/data"
)

type houseViewPageData struct {
	pageData

	House data.HouseProfile
	Area  string
	Lot   string
}

// houseFormPageData is also used to redisplay the form with the submitted
// values and a field error after a validation failure.
type houseFormPageData struct {
	pageData

	House       data.HouseProfile
	AreaTitle   string
	LotTitle    string
	IsNew       bool
	Error       string
	AreaInput   string
	LotInput    string
	TaxInput    string
	HOAFeeInput string
}

func (s *Server) handleHouseView(w http.ResponseWriter, r *http.Request) {
	house, err := s.store.HouseProfile()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Redirect(w, r, "/house/edit", http.StatusSeeOther)
			return
		}
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "house_view.html", houseViewPageData{
		pageData: pageData{Title: "House", Nav: "house"},
		House:    house,
		Area:     data.FormatArea(house.SquareFeet, s.units),
		Lot:      data.FormatLotArea(house.LotSquareFeet, s.units),
	})
}

func (s *Server) handleHouseEditForm(w http.ResponseWriter, _ *http.Request) {
	house, err := s.store.HouseProfile()
	isNew := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNew {
		s.renderError(w, err)
		return
	}
	s.renderHouseForm(w, http.StatusOK, houseFormPageData{
		pageData:    pageData{Title: "House", Nav: "house"},
		House:       house,
		AreaTitle:   data.AreaFormTitle(s.units),
		LotTitle:    data.LotAreaFormTitle(s.units),
		IsNew:       isNew,
		AreaInput:   formatOptionalInt(data.SqFtToDisplayInt(house.SquareFeet, s.units)),
		LotInput:    formatOptionalInt(data.SqFtToDisplayInt(house.LotSquareFeet, s.units)),
		TaxInput:    s.cur.FormatOptionalCents(house.PropertyTaxCents),
		HOAFeeInput: s.cur.FormatOptionalCents(house.HOAFeeCents),
	})
}

func (s *Server) renderHouseForm(w http.ResponseWriter, status int, page houseFormPageData) {
	s.render(w, status, "house_form.html", page)
}

// handleHouseSubmit parses and validates the house form the same way
// internal/app/forms.go's saveHouseFormData does, reusing the same
// data.Parse* / Currency.Parse* helpers so validation and stored values
// match the TUI exactly.
func (s *Server) handleHouseSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}

	_, err := s.store.HouseProfile()
	isNew := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNew {
		s.renderError(w, err)
		return
	}

	profile, formErr := s.parseHouseForm(r)
	if formErr != nil {
		s.renderHouseForm(w, http.StatusUnprocessableEntity, houseFormPageData{
			pageData:    pageData{Title: "House", Nav: "house"},
			House:       profile,
			AreaTitle:   data.AreaFormTitle(s.units),
			LotTitle:    data.LotAreaFormTitle(s.units),
			IsNew:       isNew,
			Error:       formErr.Error(),
			AreaInput:   r.FormValue("square_feet"),
			LotInput:    r.FormValue("lot_square_feet"),
			TaxInput:    r.FormValue("property_tax"),
			HOAFeeInput: r.FormValue("hoa_fee"),
		})
		return
	}

	if isNew {
		err = s.store.CreateHouseProfile(profile)
	} else {
		err = s.store.UpdateHouseProfile(profile)
	}
	if err != nil {
		s.renderError(w, err)
		return
	}
	http.Redirect(w, r, "/house", http.StatusSeeOther)
}

func (s *Server) parseHouseForm(r *http.Request) (data.HouseProfile, error) {
	yearBuilt, err := data.ParseOptionalInt(r.FormValue("year_built"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("Year Built", err)
	}
	sqftDisplay, err := data.ParseOptionalInt(r.FormValue("square_feet"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError(data.AreaFormTitle(s.units), err)
	}
	lotDisplay, err := data.ParseOptionalInt(r.FormValue("lot_square_feet"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError(data.LotAreaFormTitle(s.units), err)
	}
	bedrooms, err := data.ParseOptionalInt(r.FormValue("bedrooms"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("Bedrooms", err)
	}
	bathrooms, err := data.ParseOptionalFloat(r.FormValue("bathrooms"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("Bathrooms", err)
	}
	insuranceRenewal, err := data.ParseOptionalDate(r.FormValue("insurance_renewal"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("Insurance Renewal", err)
	}
	propertyTax, err := s.cur.ParseOptionalCents(r.FormValue("property_tax"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("Property Tax", err)
	}
	hoaFee, err := s.cur.ParseOptionalCents(r.FormValue("hoa_fee"))
	if err != nil {
		return data.HouseProfile{}, data.FieldError("HOA Fee", err)
	}

	return data.HouseProfile{
		Nickname:         strings.TrimSpace(r.FormValue("nickname")),
		AddressLine1:     strings.TrimSpace(r.FormValue("address_line1")),
		AddressLine2:     strings.TrimSpace(r.FormValue("address_line2")),
		City:             strings.TrimSpace(r.FormValue("city")),
		State:            strings.TrimSpace(r.FormValue("state")),
		PostalCode:       strings.TrimSpace(r.FormValue("postal_code")),
		YearBuilt:        yearBuilt,
		SquareFeet:       data.DisplayIntToSqFt(sqftDisplay, s.units),
		LotSquareFeet:    data.DisplayIntToSqFt(lotDisplay, s.units),
		Bedrooms:         bedrooms,
		Bathrooms:        bathrooms,
		FoundationType:   strings.TrimSpace(r.FormValue("foundation_type")),
		WiringType:       strings.TrimSpace(r.FormValue("wiring_type")),
		RoofType:         strings.TrimSpace(r.FormValue("roof_type")),
		ExteriorType:     strings.TrimSpace(r.FormValue("exterior_type")),
		HeatingType:      strings.TrimSpace(r.FormValue("heating_type")),
		CoolingType:      strings.TrimSpace(r.FormValue("cooling_type")),
		WaterSource:      strings.TrimSpace(r.FormValue("water_source")),
		SewerType:        strings.TrimSpace(r.FormValue("sewer_type")),
		ParkingType:      strings.TrimSpace(r.FormValue("parking_type")),
		BasementType:     strings.TrimSpace(r.FormValue("basement_type")),
		InsuranceCarrier: strings.TrimSpace(r.FormValue("insurance_carrier")),
		InsurancePolicy:  strings.TrimSpace(r.FormValue("insurance_policy")),
		InsuranceRenewal: insuranceRenewal,
		PropertyTaxCents: propertyTax,
		HOAName:          strings.TrimSpace(r.FormValue("hoa_name")),
		HOAFeeCents:      hoaFee,
	}, nil
}
