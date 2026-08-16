// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
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

	Overdue            []maintenanceUrgency
	Upcoming           []maintenanceUrgency
	ActiveProjects     []data.Project
	OpenIncidents      []data.Incident
	ExpiringWarranties []warrantyStatus
	YTDServiceSpend    string
	TotalProjectSpend  string
	Currency           string
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
	if _, err := s.store.HouseProfile(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Redirect(w, r, "/house/edit", http.StatusSeeOther)
			return
		}
		s.renderError(w, fmt.Errorf("load house profile: %w", err))
		return
	}

	now := time.Now()
	page := dashboardPageData{
		pageData: pageData{Title: "Dashboard", Nav: "dashboard"},
		Currency: s.cur.Code(),
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
