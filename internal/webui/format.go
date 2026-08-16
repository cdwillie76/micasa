// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package webui

import (
	"strconv"
	"time"
)

// formatOptionalInt renders a zero value as an empty string, matching how
// the TUI's forms show unset numeric fields blank rather than "0".
func formatOptionalInt(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// formatOptionalDate renders a nil date as an empty string, and a non-nil
// date in the same YYYY-MM-DD layout data.ParseOptionalDate accepts back.
func formatOptionalDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
