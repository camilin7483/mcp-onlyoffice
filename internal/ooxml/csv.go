package ooxml

import (
	"encoding/csv"
	"fmt"
	"strings"
)

func ParseCSV(content string) ([]string, [][]string, error) {
	r := csv.NewReader(strings.NewReader(content))
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) == 0 {
		return nil, [][]string{}, nil
	}
	var data [][]string
	if len(records) > 1 {
		data = records[1:]
	}
	return records[0], data, nil
}

func FormatCSV(headers []string, rows [][]string) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	if len(headers) > 0 {
		if err := w.Write(headers); err != nil {
			return "", err
		}
	}
	if err := w.WriteAll(rows); err != nil {
		return "", err
	}
	w.Flush()
	return b.String(), w.Error()
}
