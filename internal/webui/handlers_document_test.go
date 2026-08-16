// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/micasa-dev/micasa/internal/data"
)

func TestDocumentList_EmptyState(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, documentURL, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "No documents yet.")
}

func TestDocumentNewForm_Renders(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, documentURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Add Document")
	require.Contains(t, rec.Body.String(), `<input type="file" name="file" required>`)
}

func TestDocumentForm_CreateFlow(t *testing.T) {
	srv := newTestServer(t)

	rec := doMultipart(t, srv, documentURL,
		map[string]string{"title": "Warranty Card", "notes": "Keep for records"},
		"file", "warranty.txt", []byte("warranty details"),
	)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, documentURL, rec.Header().Get("Location"))

	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "Warranty Card", docs[0].Title)
	require.Equal(t, "warranty.txt", docs[0].FileName)
	require.Equal(t, "text/plain; charset=utf-8", docs[0].MIMEType)
	require.Equal(t, int64(len("warranty details")), docs[0].SizeBytes)
	require.NotEmpty(t, docs[0].ChecksumSHA256)
}

func TestDocumentForm_TitleDefaultsToFileName(t *testing.T) {
	srv := newTestServer(t)

	rec := doMultipart(t, srv, documentURL,
		map[string]string{}, "file", "manual.pdf", []byte("%PDF-1.4 fake"),
	)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "manual.pdf", docs[0].Title)
}

func TestDocumentForm_CreateWithEntityLink(t *testing.T) {
	srv := newTestServer(t)
	vendor := data.Vendor{Name: "Linked Vendor"}
	require.NoError(t, srv.store.CreateVendor(&vendor))

	rec := doMultipart(
		t,
		srv,
		documentURL,
		map[string]string{
			"title":  "Vendor Contract",
			"entity": entityOptionKey(data.DocumentEntityVendor, vendor.ID),
		},
		"file",
		"contract.txt",
		[]byte("contract text"),
	)

	require.Equal(t, http.StatusSeeOther, rec.Code)

	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, data.DocumentEntityVendor, docs[0].EntityKind)
	require.Equal(t, vendor.ID, docs[0].EntityID)

	listRec := do(t, srv, http.MethodGet, documentURL, nil)
	require.Contains(t, listRec.Body.String(), "Vendor: Linked Vendor")
}

func TestDocumentEntityOptions_CoverAllLinkableKinds(t *testing.T) {
	srv := newTestServer(t)

	appliance := data.Appliance{Name: "Fridge"}
	require.NoError(t, srv.store.CreateAppliance(&appliance))
	require.NoError(t, srv.store.CreateIncident(&data.Incident{
		Title: "Fridge leak", Status: data.IncidentStatusOpen,
		Severity: data.IncidentSeveritySoon, DateNoticed: mustParseDate(t, "2026-01-01"),
	}))
	categoryID := firstMaintenanceCategoryID(t, srv)
	require.NoError(t, srv.store.CreateMaintenance(&data.MaintenanceItem{
		Name: "Clean coils", CategoryID: categoryID,
	}))
	project := data.Project{Title: "Kitchen Reno", ProjectTypeID: firstProjectTypeID(t, srv)}
	require.NoError(t, srv.store.CreateProject(&project))
	vendor := data.Vendor{Name: "Appliance Repair Co"}
	require.NoError(t, srv.store.CreateVendor(&vendor))
	require.NoError(t, srv.store.CreateQuote(&data.Quote{
		ProjectID: project.ID, TotalCents: 1000,
	}, vendor))

	rec := do(t, srv, http.MethodGet, documentURL+"/new", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Appliance: Fridge")
	require.Contains(t, body, "Incident: Fridge leak")
	require.Contains(t, body, "Maintenance: Clean coils")
	require.Contains(t, body, "Project: Kitchen Reno")
	require.Contains(t, body, "Quote: Kitchen Reno / Appliance Repair Co")
	require.Contains(t, body, "Vendor: Appliance Repair Co")
}

func TestDetectMIMEType_FallsBackToExtensionForUnknownContent(t *testing.T) {
	// Content bytes that sniff as application/octet-stream, so detection
	// must fall back to the file extension.
	garbage := []byte{0x00, 0x01, 0x02, 0x03}

	require.Equal(t, "text/csv", detectMIMEType("data.csv", garbage))
	require.Equal(t, "application/json", detectMIMEType("data.json", garbage))
	require.Equal(t, "text/markdown", detectMIMEType("notes.md", garbage))
	require.Equal(t, "application/octet-stream", detectMIMEType("data.bin", garbage))
}

func TestDocumentForm_MissingFileRedisplaysFormWithoutSaving(t *testing.T) {
	srv := newTestServer(t)

	rec := doMultipart(t, srv, documentURL,
		map[string]string{"title": "No File"}, "", "", nil,
	)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Contains(t, rec.Body.String(), "a file is required")

	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Empty(t, docs)
}

func TestDocumentForm_UpdateMetadataOnlyPreservesFile(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(
		t,
		srv,
		documentURL,
		map[string]string{
			"title": "Original Title",
		},
		"file",
		"original.txt",
		[]byte("original bytes"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	id := docs[0].ID

	updateRec := doMultipart(t, srv, documentURL+"/"+id,
		map[string]string{"title": "Updated Title"}, "", "", nil,
	)
	require.Equal(t, http.StatusSeeOther, updateRec.Code)

	updated, err := srv.store.GetDocument(id)
	require.NoError(t, err)
	require.Equal(t, "Updated Title", updated.Title)
	require.Equal(t, "original.txt", updated.FileName)
	require.Equal(t, []byte("original bytes"), updated.Data)
}

func TestDocumentForm_UpdateReplacesFile(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(t, srv, documentURL,
		map[string]string{"title": "Doc"}, "file", "v1.txt", []byte("version one"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	id := docs[0].ID

	updateRec := doMultipart(t, srv, documentURL+"/"+id,
		map[string]string{"title": "Doc"}, "file", "v2.txt", []byte("version two"),
	)
	require.Equal(t, http.StatusSeeOther, updateRec.Code)

	updated, err := srv.store.GetDocument(id)
	require.NoError(t, err)
	require.Equal(t, "v2.txt", updated.FileName)
	require.Equal(t, []byte("version two"), updated.Data)
}

func TestDocumentDownload_ServesFileWithHeaders(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(
		t,
		srv,
		documentURL,
		map[string]string{
			"title": "Download Me",
		},
		"file",
		"download.txt",
		[]byte("downloadable content"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	id := docs[0].ID

	rec := do(t, srv, http.MethodGet, documentURL+"/"+id+"/download", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "downloadable content", rec.Body.String())
	require.Contains(t, rec.Header().Get("Content-Disposition"), "download.txt")
	require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
}

func TestDocumentDownload_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, documentURL+"/does-not-exist/download", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDocumentDeleteAndRestore_Flow(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(t, srv, documentURL,
		map[string]string{"title": "Deletable Doc"}, "file", "d.txt", []byte("data"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	id := docs[0].ID

	deleteRec := do(t, srv, http.MethodPost, documentURL+"/"+id+"/delete", url.Values{})
	require.Equal(t, http.StatusSeeOther, deleteRec.Code)

	active, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Empty(t, active)

	restoreRec := do(t, srv, http.MethodPost, documentURL+"/"+id+"/restore", url.Values{})
	require.Equal(t, http.StatusSeeOther, restoreRec.Code)

	restored, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	require.Len(t, restored, 1)
}

func TestDocumentEditForm_UnknownIDReturns404(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, http.MethodGet, documentURL+"/does-not-exist/edit", nil)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDocumentEditForm_PrefillsExistingValues(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(
		t,
		srv,
		documentURL,
		map[string]string{
			"title": "Prefill Doc",
			"notes": "Some notes",
		},
		"file",
		"p.txt",
		[]byte("data"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	id := docs[0].ID

	rec := do(t, srv, http.MethodGet, documentURL+"/"+id+"/edit", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="Prefill Doc"`)
	require.Contains(t, rec.Body.String(), "Some notes")
	require.Contains(t, rec.Body.String(), "p.txt")
}

func TestDocumentNewForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, documentURL+"/new", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDocumentEditForm_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	createRec := doMultipart(t, srv, documentURL,
		map[string]string{"title": "Some Doc"}, "file", "s.txt", []byte("data"),
	)
	require.Equal(t, http.StatusSeeOther, createRec.Code)
	docs, err := srv.store.ListDocuments(false)
	require.NoError(t, err)
	id := docs[0].ID
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, documentURL+"/"+id+"/edit", nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDocumentDelete_StoreErrorRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodPost, documentURL+"/does-not-exist/delete", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")
}

func TestDocumentRestore_StoreErrorRedirectsWithError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodPost, documentURL+"/does-not-exist/restore", url.Values{})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "error=")
}

func TestDocumentList_StoreErrorRendersServerError(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.store.Close())

	rec := do(t, srv, http.MethodGet, documentURL, nil)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
