// Package finance owns money, entries, currencies, and exchange rates.
package finance

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidAmount is returned when a decimal amount cannot be represented
// exactly at the currency's minor-unit precision.
var ErrInvalidAmount = errors.New("invalid amount")

// ParseAmount parses a positive decimal string into integer minor units.
// Fractional digits beyond the currency exponent are rejected rather than
// silently rounded.
func ParseAmount(raw string, minorUnits int32) (int64, error) {
	if minorUnits < 0 || minorUnits > 4 {
		return 0, fmt.Errorf("%w: unsupported minor units %d", ErrInvalidAmount, minorUnits)
	}

	amount := strings.TrimSpace(raw)
	if amount == "" {
		return 0, fmt.Errorf("%w: amount is required", ErrInvalidAmount)
	}
	if strings.HasPrefix(amount, "+") || strings.HasPrefix(amount, "-") {
		return 0, fmt.Errorf("%w: amount must be positive", ErrInvalidAmount)
	}

	parts := strings.Split(amount, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("%w: amount must contain at most one decimal point", ErrInvalidAmount)
	}
	whole := parts[0]
	if whole == "" || !digitsOnly(whole) {
		return 0, fmt.Errorf("%w: amount must contain digits", ErrInvalidAmount)
	}

	fraction := ""
	if len(parts) == 2 {
		if minorUnits == 0 {
			return 0, fmt.Errorf("%w: currency does not use fractional units", ErrInvalidAmount)
		}
		fraction = parts[1]
		if fraction == "" || !digitsOnly(fraction) {
			return 0, fmt.Errorf("%w: fraction must contain digits", ErrInvalidAmount)
		}
		if len(fraction) > int(minorUnits) {
			return 0, fmt.Errorf("%w: at most %d decimal places are allowed", ErrInvalidAmount, minorUnits)
		}
	}
	if minorUnits > 0 {
		fraction += strings.Repeat("0", int(minorUnits)-len(fraction))
	}

	minor, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: amount is too large", ErrInvalidAmount)
	}
	if minor <= 0 {
		return 0, fmt.Errorf("%w: amount must be greater than zero", ErrInvalidAmount)
	}
	return minor, nil
}

// FormatAmount formats minor units as an ungrouped decimal string suitable for
// API responses and form inputs.
func FormatAmount(minor int64, minorUnits int32) string {
	if minorUnits < 0 || minorUnits > 4 {
		return "0"
	}
	integer, fraction, negative, ok := splitMinor(minor, minorUnits)
	if !ok {
		return "0"
	}
	result := integer
	if minorUnits > 0 {
		result += "." + fraction
	}
	if negative {
		result = "-" + result
	}
	return result
}

// DisplayAmount formats minor units with grouped thousands for the HTML UI.
func DisplayAmount(minor int64, minorUnits int32) string {
	integer, fraction, negative, ok := splitMinor(minor, minorUnits)
	if !ok {
		return "0"
	}
	grouped := groupDigits(integer)
	if minorUnits > 0 {
		grouped += "." + fraction
	}
	if negative {
		grouped = "-" + grouped
	}
	return grouped
}

func splitMinor(minor int64, minorUnits int32) (integer, fraction string, negative, ok bool) {
	if minorUnits < 0 || minorUnits > 4 {
		return "", "", false, false
	}

	negative = minor < 0
	var absolute uint64
	if negative {
		absolute = uint64(-(minor + 1)) + 1
	} else {
		absolute = uint64(minor)
	}

	divisor := uint64(1)
	for range minorUnits {
		divisor *= 10
	}
	whole := absolute / divisor
	remainder := absolute % divisor

	if minorUnits == 0 {
		return strconv.FormatUint(whole, 10), "", negative, true
	}
	fraction = fmt.Sprintf("%0*d", int(minorUnits), remainder)
	return strconv.FormatUint(whole, 10), fraction, negative, true
}

func groupDigits(value string) string {
	if len(value) <= 3 {
		return value
	}
	var builder strings.Builder
	first := len(value) % 3
	if first == 0 {
		first = 3
	}
	builder.WriteString(value[:first])
	for i := first; i < len(value); i += 3 {
		builder.WriteByte(',')
		builder.WriteString(value[i : i+3])
	}
	return builder.String()
}

func digitsOnly(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return len(value) > 0
}
