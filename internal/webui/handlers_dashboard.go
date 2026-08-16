// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/micasa-dev/micasa/internal/data"
)

// dashboardPageData is the view-model for the dashboard page. Overdue/
// Upcoming windowing mirrors internal/app/dashboard.go's loadDashboardAt
// (overdue: past due; upcoming: due within 30 days), kept independently
// here since each UI owns its own presentation windowing over the shared
// raw queries in internal/data.
type dashboardPageData struct {
	pageData

	HousePill          string
	HouseSummary       string
	Overdue            []maintenanceUrgency
	Upcoming           []maintenanceUrgency
	ActiveProjects     []data.Project
	OpenIncidents      []data.Incident
	ExpiringWarranties []warrantyStatus
	YTDServiceSpend    string
	TotalProjectSpend  string
	Currency           string
}

// houseSummaryLine mirrors internal/app/house.go's houseCollapsed layout
// (address · city, state · Nbd/Nba · area · year), joined with " . "
// instead of lipgloss styling, so the web dashboard's house strip reads
// the same as the TUI's collapsed house header.
func houseSummaryLine(house data.HouseProfile, units data.UnitSystem) string {
	var parts []string
	if addr := strings.TrimSpace(house.AddressLine1); addr != "" {
		parts = append(parts, addr)
	}
	if cs := formatCityState(house); cs != "" {
		parts = append(parts, cs)
	}
	if bb := formatBedBath(house); bb != "" {
		parts = append(parts, bb)
	}
	if area := data.FormatArea(house.SquareFeet, units); area != "" {
		parts = append(parts, area)
	}
	if house.YearBuilt > 0 {
		parts = append(parts, strconv.Itoa(house.YearBuilt))
	}
	return strings.Join(parts, " · ")
}

func formatCityState(house data.HouseProfile) string {
	city := strings.TrimSpace(house.City)
	state := strings.TrimSpace(house.State)
	switch {
	case city != "" && state != "":
		return city + ", " + state
	case city != "":
		return city
	default:
		return state
	}
}

func formatBedBath(house data.HouseProfile) string {
	var parts []string
	if house.Bedrooms > 0 {
		parts = append(parts, strconv.Itoa(house.Bedrooms)+"bd")
	}
	if house.Bathrooms > 0 {
		parts = append(parts, formatFloatTrim(house.Bathrooms)+"ba")
	}
	return strings.Join(parts, " / ")
}

// formatFloatTrim renders a whole number without a decimal point (4 not
// 4.0) and everything else to one decimal place, matching the TUI's
// formatFloat in internal/app/house.go.
func formatFloatTrim(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

type maintenanceUrgency struct {
	Item        data.MaintenanceItem
	NextDue     time.Time
	DaysFromNow int
}

type warrantyStatus struct {
	Appliance   data.Appliance
	DaysFromNow int
}

func daysUntil(now, target time.Time) int {
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	targetDay := time.Date(
		target.Year(),
		target.Month(),
		target.Day(),
		0,
		0,
		0,
		0,
		target.Location(),
	)
	return int(targetDay.Sub(nowDay).Hours() / 24)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	house, err := s.store.HouseProfile()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Redirect(w, r, "/house/edit", http.StatusSeeOther)
			return
		}
		s.renderError(w, fmt.Errorf("load house profile: %w", err))
		return
	}

	now := time.Now()
	housePill := house.Nickname
	if housePill == "" {
		housePill = "House"
	}
	page := dashboardPageData{
		pageData:     pageData{Title: "Dashboard", Nav: "dashboard"},
		Currency:     s.cur.Code(),
		HousePill:    housePill,
		HouseSummary: houseSummaryLine(house, s.units),
	}

	items, err := s.store.ListMaintenanceWithSchedule()
	if err != nil {
		s.renderError(w, fmt.Errorf("load maintenance: %w", err))
		return
	}
	for _, item := range items {
		nextDue := data.ComputeNextDue(item.LastServicedAt, item.IntervalMonths, item.DueDate)
		if nextDue == nil {
			continue
		}
		days := daysUntil(now, *nextDue)
		entry := maintenanceUrgency{Item: item, NextDue: *nextDue, DaysFromNow: days}
		switch {
		case days < 0:
			page.Overdue = append(page.Overdue, entry)
		case days <= 30:
			page.Upcoming = append(page.Upcoming, entry)
		}
	}
	sort.Slice(page.Overdue, func(i, j int) bool {
		return page.Overdue[i].DaysFromNow < page.Overdue[j].DaysFromNow
	})
	sort.Slice(page.Upcoming, func(i, j int) bool {
		return page.Upcoming[i].DaysFromNow < page.Upcoming[j].DaysFromNow
	})

	page.ActiveProjects, err = s.store.ListActiveProjects()
	if err != nil {
		s.renderError(w, fmt.Errorf("load active projects: %w", err))
		return
	}

	page.OpenIncidents, err = s.store.ListOpenIncidents()
	if err != nil {
		s.renderError(w, fmt.Errorf("load open incidents: %w", err))
		return
	}

	appliances, err := s.store.ListExpiringWarranties(now, 30*24*time.Hour, 90*24*time.Hour)
	if err != nil {
		s.renderError(w, fmt.Errorf("load warranties: %w", err))
		return
	}
	for _, a := range appliances {
		if a.WarrantyExpiry == nil {
			continue
		}
		page.ExpiringWarranties = append(page.ExpiringWarranties, warrantyStatus{
			Appliance:   a,
			DaysFromNow: daysUntil(now, *a.WarrantyExpiry),
		})
	}

	ytd, err := s.store.YTDServiceSpendCents(
		time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location()),
	)
	if err != nil {
		s.renderError(w, fmt.Errorf("load YTD spend: %w", err))
		return
	}
	page.YTDServiceSpend = s.cur.FormatCents(ytd)

	totalProjectSpend, err := s.store.TotalProjectSpendCents()
	if err != nil {
		s.renderError(w, fmt.Errorf("load project spend: %w", err))
		return
	}
	page.TotalProjectSpend = s.cur.FormatCents(totalProjectSpend)

	s.render(w, http.StatusOK, "dashboard.html", page)
}
