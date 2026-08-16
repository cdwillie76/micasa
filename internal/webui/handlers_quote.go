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
	quoteNav  = "quotes"
	quotesURL = "/quotes"
)

type quoteListPageData struct {
	pageData

	Quotes         []quoteRow
	IncludeDeleted bool
	Error          string
}

// quoteRow pairs a Quote with its display-formatted total, since money
// formatting depends on Server.cur.
type quoteRow struct {
	data.Quote

	Total string
}

type quoteFormPageData struct {
	pageData

	Quote        data.Quote
	Projects     []data.Project
	Vendors      []data.Vendor
	IsNew        bool
	Error        string
	Total        string
	Labor        string
	Materials    string
	Other        string
	ReceivedDate string
}

func (s *Server) handleQuoteList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	quotes, err := s.store.ListQuotes(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load quotes: %w", err))
		return
	}
	rows := make([]quoteRow, len(quotes))
	for i, q := range quotes {
		rows[i] = quoteRow{Quote: q, Total: s.cur.FormatCents(q.TotalCents)}
	}
	s.render(w, http.StatusOK, "quote_list.html", quoteListPageData{
		pageData:       pageData{Title: "Quotes", Nav: quoteNav},
		Quotes:         rows,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

// quoteFormLookups loads the project/vendor option lists shared by the new,
// edit, and error-redisplay quote form pages.
func (s *Server) quoteFormLookups() ([]data.Project, []data.Vendor, error) {
	projects, err := s.store.ListProjects(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load projects: %w", err)
	}
	vendors, err := s.store.ListVendors(false)
	if err != nil {
		return nil, nil, fmt.Errorf("load vendors: %w", err)
	}
	return projects, vendors, nil
}

func (s *Server) handleQuoteNewForm(w http.ResponseWriter, _ *http.Request) {
	projects, vendors, err := s.quoteFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	quote := data.Quote{}
	if len(projects) > 0 {
		quote.ProjectID = projects[0].ID
	}
	if len(vendors) > 0 {
		quote.VendorID = vendors[0].ID
	}
	s.render(w, http.StatusOK, "quote_form.html", quoteFormPageData{
		pageData: pageData{Title: "New Quote", Nav: quoteNav},
		Quote:    quote,
		Projects: projects,
		Vendors:  vendors,
		IsNew:    true,
	})
}

func (s *Server) handleQuoteEditForm(w http.ResponseWriter, r *http.Request) {
	quote, err := s.store.GetQuote(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load quote: %w", err))
		return
	}
	projects, vendors, err := s.quoteFormLookups()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "quote_form.html", quoteFormPageData{
		pageData:     pageData{Title: "Edit Quote", Nav: quoteNav},
		Quote:        quote,
		Projects:     projects,
		Vendors:      vendors,
		Total:        s.cur.FormatCents(quote.TotalCents),
		Labor:        s.cur.FormatOptionalCents(quote.LaborCents),
		Materials:    s.cur.FormatOptionalCents(quote.MaterialsCents),
		Other:        s.cur.FormatOptionalCents(quote.OtherCents),
		ReceivedDate: formatOptionalDate(quote.ReceivedDate),
	})
}

// parseQuoteForm mirrors internal/app/forms.go's parseQuoteFormData. Unlike
// the TUI (which takes a free-text vendor name and finds-or-creates it), the
// web form selects an existing vendor by ID and resolves its name -- both
// paths end up calling Store.CreateQuote/UpdateQuote's find-or-create-by-name
// vendor resolution, so an existing vendor is always reused, never duplicated.
func (s *Server) parseQuoteForm(r *http.Request) (data.Quote, data.Vendor, error) {
	projectID := r.FormValue("project_id")
	if projectID == "" {
		return data.Quote{}, data.Vendor{}, errors.New("project is required")
	}
	vendor, err := s.store.GetVendor(r.FormValue("vendor_id"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, errors.New("vendor is required")
	}
	total, err := s.cur.ParseRequiredCents(r.FormValue("total"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, data.FieldError("Total", err)
	}
	labor, err := s.cur.ParseOptionalCents(r.FormValue("labor"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, data.FieldError("Labor", err)
	}
	materials, err := s.cur.ParseOptionalCents(r.FormValue("materials"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, data.FieldError("Materials", err)
	}
	other, err := s.cur.ParseOptionalCents(r.FormValue("other"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, data.FieldError("Other", err)
	}
	received, err := data.ParseOptionalDate(r.FormValue("received_date"))
	if err != nil {
		return data.Quote{}, data.Vendor{}, data.FieldError("Received Date", err)
	}
	quote := data.Quote{
		ProjectID:      projectID,
		TotalCents:     total,
		LaborCents:     labor,
		MaterialsCents: materials,
		OtherCents:     other,
		ReceivedDate:   received,
		Notes:          strings.TrimSpace(r.FormValue("notes")),
	}
	return quote, vendor, nil
}

func (s *Server) quoteFormErrorPage(
	title string, quote data.Quote, r *http.Request, err error,
) (quoteFormPageData, error) {
	projects, vendors, lookupErr := s.quoteFormLookups()
	if lookupErr != nil {
		return quoteFormPageData{}, lookupErr
	}
	return quoteFormPageData{
		pageData:     pageData{Title: title, Nav: quoteNav},
		Quote:        quote,
		Projects:     projects,
		Vendors:      vendors,
		IsNew:        title == "New Quote",
		Error:        err.Error(),
		Total:        r.FormValue("total"),
		Labor:        r.FormValue("labor"),
		Materials:    r.FormValue("materials"),
		Other:        r.FormValue("other"),
		ReceivedDate: r.FormValue("received_date"),
	}, nil
}

func (s *Server) handleQuoteCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	quote, vendor, formErr := s.parseQuoteForm(r)
	if formErr != nil {
		quote.VendorID = r.FormValue("vendor_id")
		page, err := s.quoteFormErrorPage("New Quote", quote, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "quote_form.html", page)
		return
	}
	if err := s.store.CreateQuote(&quote, vendor); err != nil {
		page, pageErr := s.quoteFormErrorPage("New Quote", quote, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "quote_form.html", page)
		return
	}
	http.Redirect(w, r, quotesURL, http.StatusSeeOther)
}

func (s *Server) handleQuoteUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	quote, vendor, formErr := s.parseQuoteForm(r)
	if formErr != nil {
		quote.ID = id
		quote.VendorID = r.FormValue("vendor_id")
		page, err := s.quoteFormErrorPage("Edit Quote", quote, r, formErr)
		if err != nil {
			s.renderError(w, err)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "quote_form.html", page)
		return
	}
	quote.ID = id
	if err := s.store.UpdateQuote(quote, vendor); err != nil {
		page, pageErr := s.quoteFormErrorPage("Edit Quote", quote, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "quote_form.html", page)
		return
	}
	http.Redirect(w, r, quotesURL, http.StatusSeeOther)
}

func (s *Server) handleQuoteDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteQuote(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, quotesURL, err)
		return
	}
	http.Redirect(w, r, quotesURL, http.StatusSeeOther)
}

func (s *Server) handleQuoteRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreQuote(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, quotesURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, quotesURL+"?deleted=1", http.StatusSeeOther)
}
