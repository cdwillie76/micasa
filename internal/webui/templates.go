// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

//go:embed templates/*.html
var templatesFS embed.FS

// templates holds the parsed page templates. Each page template is parsed
// together with layout.html so {{define "content"}} blocks in the page
// fill the shared shell.
type templates struct {
	pages map[string]*template.Template
}

func loadTemplates() (*templates, error) {
	layoutSrc, err := fs.ReadFile(templatesFS, "templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("read layout template: %w", err)
	}

	pageNames := []string{
		"dashboard.html", "house_view.html", "house_form.html",
		"vendor_list.html", "vendor_form.html",
		"appliance_list.html", "appliance_form.html",
		"project_list.html", "project_form.html",
		"incident_list.html", "incident_form.html",
		"maintenance_list.html", "maintenance_form.html",
		"quote_list.html", "quote_form.html",
		"servicelog_list.html", "servicelog_form.html",
	}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("layout.html").Parse(string(layoutSrc))
		if err != nil {
			return nil, fmt.Errorf("parse layout for %s: %w", name, err)
		}
		t, err = t.ParseFS(templatesFS, "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = t
	}
	return &templates{pages: pages}, nil
}
