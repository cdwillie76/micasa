// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"gorm.io/gorm"

	"github.com/micasa-dev/micasa/internal/data"
)

const (
	documentNav = "documents"
	documentURL = "/documents"

	// maxUploadOverhead covers multipart form field overhead (title, notes,
	// entity selection, boundaries) on top of the file itself.
	maxUploadOverhead = 1 << 20 // 1 MiB
)

// maxUploadBytes bounds an incoming request body to store.MaxDocumentSize
// plus form overhead, capped so the uint64->int64 conversion (required by
// http.MaxBytesReader) can never overflow.
func maxUploadBytes(maxDocSize uint64) int64 {
	if maxDocSize > math.MaxInt64-maxUploadOverhead {
		return math.MaxInt64
	}
	return int64(maxDocSize) + maxUploadOverhead
}

// documentEntityOption is one selectable link target in the "Entity" picker,
// matching internal/app/forms.go's documentEntityOptions -- the same six
// linkable kinds (service log entries are excluded there too), each labeled
// "Kind: Name" instead of the TUI's colored letter-prefix styling.
type documentEntityOption struct {
	Kind  string
	ID    string
	Label string
}

func entityOptionKey(kind, id string) string { return kind + "|" + id }

// documentEntityOptions loads every linkable entity across all documentable
// kinds, in the same order internal/app/forms.go's documentEntityOptions
// uses (appliances, incidents, maintenance, projects, quotes, vendors).
func (s *Server) documentEntityOptions() ([]documentEntityOption, error) {
	var opts []documentEntityOption

	appliances, err := s.store.ListAppliances(false)
	if err != nil {
		return nil, fmt.Errorf("list appliances: %w", err)
	}
	for _, a := range appliances {
		label := a.Name
		if a.Brand != "" {
			label = fmt.Sprintf("%s (%s)", label, a.Brand)
		}
		opts = append(
			opts,
			documentEntityOption{data.DocumentEntityAppliance, a.ID, "Appliance: " + label},
		)
	}

	incidents, err := s.store.ListIncidents(false)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	for _, inc := range incidents {
		opts = append(
			opts,
			documentEntityOption{data.DocumentEntityIncident, inc.ID, "Incident: " + inc.Title},
		)
	}

	items, err := s.store.ListMaintenance(false)
	if err != nil {
		return nil, fmt.Errorf("list maintenance items: %w", err)
	}
	for _, item := range items {
		opts = append(
			opts,
			documentEntityOption{
				data.DocumentEntityMaintenance,
				item.ID,
				"Maintenance: " + item.Name,
			},
		)
	}

	projects, err := s.store.ListProjects(false)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	for _, p := range projects {
		opts = append(
			opts,
			documentEntityOption{data.DocumentEntityProject, p.ID, "Project: " + p.Title},
		)
	}

	quotes, err := s.store.ListQuotes(false)
	if err != nil {
		return nil, fmt.Errorf("list quotes: %w", err)
	}
	for _, q := range quotes {
		label := fmt.Sprintf("Quote: %s / %s", q.Project.Title, q.Vendor.Name)
		opts = append(opts, documentEntityOption{data.DocumentEntityQuote, q.ID, label})
	}

	vendors, err := s.store.ListVendors(false)
	if err != nil {
		return nil, fmt.Errorf("list vendors: %w", err)
	}
	for _, v := range vendors {
		label := v.Name
		if v.ContactName != "" {
			label = fmt.Sprintf("%s (%s)", label, v.ContactName)
		}
		opts = append(
			opts,
			documentEntityOption{data.DocumentEntityVendor, v.ID, "Vendor: " + label},
		)
	}

	return opts, nil
}

type documentListPageData struct {
	pageData

	Documents      []documentRow
	IncludeDeleted bool
	Error          string
}

// documentRow pairs a Document with its resolved entity label and
// human-readable size, since neither is stored directly on the model.
type documentRow struct {
	data.Document

	EntityLabel string
	Size        string
}

func (s *Server) handleDocumentList(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("deleted") == "1"
	docs, err := s.store.ListDocuments(includeDeleted)
	if err != nil {
		s.renderError(w, fmt.Errorf("load documents: %w", err))
		return
	}
	opts, err := s.documentEntityOptions()
	if err != nil {
		s.renderError(w, err)
		return
	}
	labels := make(map[string]string, len(opts))
	for _, o := range opts {
		labels[entityOptionKey(o.Kind, o.ID)] = o.Label
	}
	rows := make([]documentRow, len(docs))
	for i, d := range docs {
		size := uint64(d.SizeBytes) //nolint:gosec // SizeBytes is always non-negative
		rows[i] = documentRow{
			Document:    d,
			EntityLabel: labels[entityOptionKey(d.EntityKind, d.EntityID)],
			Size:        humanize.IBytes(size),
		}
	}
	s.render(w, http.StatusOK, "document_list.html", documentListPageData{
		pageData:       pageData{Title: "Documents", Nav: documentNav, Wide: true},
		Documents:      rows,
		IncludeDeleted: includeDeleted,
		Error:          r.URL.Query().Get("error"),
	})
}

type documentFormPageData struct {
	pageData

	Document       data.Document
	EntityOptions  []documentEntityOption
	SelectedEntity string
	IsNew          bool
	Error          string
	MaxUploadSize  string
}

func (s *Server) handleDocumentNewForm(w http.ResponseWriter, _ *http.Request) {
	opts, err := s.documentEntityOptions()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "document_form.html", documentFormPageData{
		pageData:      pageData{Title: "New Document", Nav: documentNav},
		EntityOptions: opts,
		IsNew:         true,
		MaxUploadSize: humanize.IBytes(s.store.MaxDocumentSize()),
	})
}

func (s *Server) handleDocumentEditForm(w http.ResponseWriter, r *http.Request) {
	doc, err := s.store.GetDocumentMetadata(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load document: %w", err))
		return
	}
	opts, err := s.documentEntityOptions()
	if err != nil {
		s.renderError(w, err)
		return
	}
	s.render(w, http.StatusOK, "document_form.html", documentFormPageData{
		pageData:       pageData{Title: "Edit Document", Nav: documentNav},
		Document:       doc,
		EntityOptions:  opts,
		SelectedEntity: entityOptionKey(doc.EntityKind, doc.EntityID),
		MaxUploadSize:  humanize.IBytes(s.store.MaxDocumentSize()),
	})
}

func (s *Server) documentFormErrorPage(
	title string, doc data.Document, r *http.Request, err error,
) (documentFormPageData, error) {
	opts, lookupErr := s.documentEntityOptions()
	if lookupErr != nil {
		return documentFormPageData{}, lookupErr
	}
	return documentFormPageData{
		pageData:       pageData{Title: title, Nav: documentNav},
		Document:       doc,
		EntityOptions:  opts,
		SelectedEntity: r.FormValue("entity"),
		IsNew:          title == "New Document",
		Error:          err.Error(),
		MaxUploadSize:  humanize.IBytes(s.store.MaxDocumentSize()),
	}, nil
}

// parseDocumentEntity splits the combined "kind|id" select value produced by
// documentEntityOptions. Empty input means "not linked to any entity".
func parseDocumentEntity(combined string) (kind, id string) {
	k, i, found := strings.Cut(combined, "|")
	if !found {
		return "", ""
	}
	return k, i
}

// readUploadedFile reads the optional "file" multipart field, sniffing its
// MIME type and checksum the same way internal/app/forms.go's
// parseDocumentFormData does for a locally-picked file. Returns ok=false
// when no file was submitted (kept for edit, where it means "keep the
// existing attachment").
func readUploadedFile(r *http.Request) (
	fileName, mimeType, checksum string, fileBytes []byte, ok bool, err error,
) {
	file, header, err := r.FormFile("file")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			return "", "", "", nil, false, nil
		}
		return "", "", "", nil, false, fmt.Errorf("read uploaded file: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileBytes, err = io.ReadAll(file)
	if err != nil {
		return "", "", "", nil, false, fmt.Errorf("read uploaded file: %w", err)
	}
	sum := sha256.Sum256(fileBytes)
	return header.Filename, detectMIMEType(header.Filename, fileBytes),
		hex.EncodeToString(sum[:]), fileBytes, true, nil
}

// detectMIMEType mirrors internal/app/forms.go's detectMIMEType: sniff the
// content, falling back to extension-based detection for the types
// http.DetectContentType can't recognize.
func detectMIMEType(fileName string, fileBytes []byte) string {
	mime := http.DetectContentType(fileBytes)
	if mime != "application/octet-stream" {
		return mime
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	default:
		return mime
	}
}

func (s *Server) handleDocumentCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes(s.store.MaxDocumentSize()))
	// Body is already bounded by MaxBytesReader above, so the 32 MiB memory
	// threshold here only controls the in-memory/disk-spill split, not the
	// overall request size.
	parseErr := r.ParseMultipartForm(32 << 20) //nolint:gosec // body size is bounded above
	if parseErr != nil {
		http.Error(w, "file is too large or the form is invalid", http.StatusBadRequest)
		return
	}

	kind, id := parseDocumentEntity(r.FormValue("entity"))
	doc := data.Document{
		Title:      strings.TrimSpace(r.FormValue("title")),
		EntityKind: kind,
		EntityID:   id,
		Notes:      strings.TrimSpace(r.FormValue("notes")),
	}

	fileName, mimeType, checksum, fileBytes, ok, err := readUploadedFile(r)
	if err != nil {
		page, pageErr := s.documentFormErrorPage("New Document", doc, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "document_form.html", page)
		return
	}
	if !ok {
		page, pageErr := s.documentFormErrorPage(
			"New Document", doc, r, errors.New("a file is required"),
		)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "document_form.html", page)
		return
	}
	doc.FileName = fileName
	doc.MIMEType = mimeType
	doc.ChecksumSHA256 = checksum
	doc.Data = fileBytes
	doc.SizeBytes = int64(len(fileBytes))
	if doc.Title == "" {
		doc.Title = fileName
	}

	if err := s.store.CreateDocument(&doc); err != nil {
		page, pageErr := s.documentFormErrorPage("New Document", doc, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "document_form.html", page)
		return
	}
	http.Redirect(w, r, documentURL, http.StatusSeeOther)
}

func (s *Server) handleDocumentUpdate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes(s.store.MaxDocumentSize()))
	// Body is already bounded by MaxBytesReader above, so the 32 MiB memory
	// threshold here only controls the in-memory/disk-spill split, not the
	// overall request size.
	parseErr := r.ParseMultipartForm(32 << 20) //nolint:gosec // body size is bounded above
	if parseErr != nil {
		http.Error(w, "file is too large or the form is invalid", http.StatusBadRequest)
		return
	}

	id := r.PathValue("id")
	kind, entityID := parseDocumentEntity(r.FormValue("entity"))
	doc := data.Document{
		ID:         id,
		Title:      strings.TrimSpace(r.FormValue("title")),
		EntityKind: kind,
		EntityID:   entityID,
		Notes:      strings.TrimSpace(r.FormValue("notes")),
	}

	fileName, mimeType, checksum, fileBytes, ok, err := readUploadedFile(r)
	if err != nil {
		page, pageErr := s.documentFormErrorPage("Edit Document", doc, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "document_form.html", page)
		return
	}
	// Leaving the file field empty preserves the existing attachment --
	// Store.UpdateDocument omits file columns from the update when Data
	// is empty, so we only set these when a replacement was uploaded.
	if ok {
		doc.FileName = fileName
		doc.MIMEType = mimeType
		doc.ChecksumSHA256 = checksum
		doc.Data = fileBytes
		doc.SizeBytes = int64(len(fileBytes))
	}
	if doc.Title == "" {
		doc.Title = fileName
	}

	if err := s.store.UpdateDocument(doc); err != nil {
		page, pageErr := s.documentFormErrorPage("Edit Document", doc, r, err)
		if pageErr != nil {
			s.renderError(w, pageErr)
			return
		}
		s.render(w, http.StatusUnprocessableEntity, "document_form.html", page)
		return
	}
	http.Redirect(w, r, documentURL, http.StatusSeeOther)
}

func (s *Server) handleDocumentDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDocument(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, documentURL, err)
		return
	}
	http.Redirect(w, r, documentURL, http.StatusSeeOther)
}

func (s *Server) handleDocumentRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RestoreDocument(r.PathValue("id")); err != nil {
		s.redirectWithError(w, r, documentURL+"?deleted=1", err)
		return
	}
	http.Redirect(w, r, documentURL+"?deleted=1", http.StatusSeeOther)
}

func (s *Server) handleDocumentDownload(w http.ResponseWriter, r *http.Request) {
	doc, err := s.store.GetDocument(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, fmt.Errorf("load document: %w", err))
		return
	}
	contentType := doc.MIMEType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	fileName := doc.FileName
	if fileName == "" {
		fileName = doc.Title
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", url.PathEscape(fileName)),
	)
	w.Header().Set("Content-Length", strconv.Itoa(len(doc.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Data)
}
