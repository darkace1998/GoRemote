package mremoteng

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// parseCSV reads a header row followed by one row per connection. The column
// names must match the XML attribute names. Returns a flat list of
// rawConnection (no hierarchy — CSV is a flat export).
//
// Rows with a Type column other than "Connection" (or empty, which we treat
// as "Connection") are skipped — mRemoteNG CSV exports are connection-only.
func parseCSV(r io.Reader) ([]rawConnection, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("mremoteng: csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("mremoteng: empty csv")
	}
	header := rows[0]
	out := make([]rawConnection, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		var rc rawConnection
		for i, v := range row {
			if i >= len(header) {
				break
			}
			name := strings.TrimSpace(header[i])
			if name == "" {
				continue
			}
			assignRawAttr(&rc, name, v)
		}
		if rc.Type == "" {
			rc.Type = "Connection"
		}
		if rc.Type != "Connection" {
			continue
		}
		out = append(out, rc)
	}
	return out, nil
}

func isBlankRow(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
