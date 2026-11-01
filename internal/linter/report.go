package linter

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON renders the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	// Ensure the JSON array is never null for consumers.
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText renders findings as human-readable lines plus a summary.
func (r *Report) WriteText(w io.Writer) error {
	for _, f := range r.Findings {
		alert := ""
		if f.Alert != "" {
			alert = fmt.Sprintf("  alert=%q", f.Alert)
		}
		if _, err := fmt.Fprintf(w, "%s  %s:%d  [%s]%s\n  %s\n",
			f.Severity, f.File, f.Line, f.Rule, alert, f.Message); err != nil {
			return err
		}
		if f.Suggestion != "" {
			if _, err := fmt.Fprintf(w, "  fix: %s\n", f.Suggestion); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "%d file(s) scanned — %d warning(s), %d info\n",
		r.FilesCount, r.WarnCount, r.InfoCount)
	return err
}
