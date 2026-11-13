package web

import (
	"strings"
	"time"
)

func parseOptionalDate(raw string) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(entryDateLayout, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
