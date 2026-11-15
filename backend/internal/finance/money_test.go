package finance

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		minorUnits int32
		want       int64
		wantErr    bool
	}{
		{name: "whole number with two decimals", raw: "100", minorUnits: 2, want: 10000},
		{name: "one decimal", raw: "2.5", minorUnits: 2, want: 250},
		{name: "two decimals", raw: "2.55", minorUnits: 2, want: 255},
		{name: "smallest unit", raw: "0.01", minorUnits: 2, want: 1},
		{name: "three decimals", raw: "1.234", minorUnits: 3, want: 1234},
		{name: "four decimals", raw: "1.2345", minorUnits: 4, want: 12345},
		{name: "four decimals with padding", raw: "0.5", minorUnits: 4, want: 5000},
		{name: "zero exponent", raw: "123", minorUnits: 0, want: 123},
		{name: "whitespace", raw: "  12.5  ", minorUnits: 2, want: 1250},
		{name: "leading zeros", raw: "00012.50", minorUnits: 2, want: 1250},
		{name: "zero", raw: "0", minorUnits: 2, wantErr: true},
		{name: "zero decimals", raw: "0.00", minorUnits: 2, wantErr: true},
		{name: "negative", raw: "-1", minorUnits: 2, wantErr: true},
		{name: "leading plus", raw: "+1", minorUnits: 2, wantErr: true},
		{name: "comma grouping", raw: "1,234.56", minorUnits: 2, wantErr: true},
		{name: "two decimal points", raw: "1.2.3", minorUnits: 2, wantErr: true},
		{name: "empty fraction", raw: "1.", minorUnits: 2, wantErr: true},
		{name: "fraction on zero exponent currency", raw: "1.0", minorUnits: 0, wantErr: true},
		{name: "too many decimals", raw: "1.234", minorUnits: 2, wantErr: true},
		{name: "letters", raw: "12a", minorUnits: 2, wantErr: true},
		{name: "empty", raw: "", minorUnits: 2, wantErr: true},
		{name: "overflow", raw: strings.Repeat("9", 30), minorUnits: 2, wantErr: true},
		{name: "unsupported minor units", raw: "1.23456", minorUnits: 5, wantErr: true},
		{name: "negative minor units", raw: "1.23", minorUnits: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAmount(tt.raw, tt.minorUnits)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidAmount, "ParseAmount(%q) = %d", tt.raw, got)
				return
			}
			require.NoError(t, err, "ParseAmount(%q)", tt.raw)
			assert.Equal(t, tt.want, got, "ParseAmount(%q)", tt.raw)
		})
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		minor      int64
		minorUnits int32
		want       string
	}{
		{minor: 255, minorUnits: 2, want: "2.55"},
		{minor: 200, minorUnits: 2, want: "2.00"},
		{minor: 1, minorUnits: 2, want: "0.01"},
		{minor: 123456, minorUnits: 2, want: "1234.56"},
		{minor: 123, minorUnits: 0, want: "123"},
		{minor: 0, minorUnits: 2, want: "0.00"},
		{minor: -255, minorUnits: 2, want: "-2.55"},
		{minor: 1234, minorUnits: 3, want: "1.234"},
		{minor: 12345, minorUnits: 4, want: "1.2345"},
		{minor: 5, minorUnits: 4, want: "0.0005"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, FormatAmount(tt.minor, tt.minorUnits), "FormatAmount(%d, %d)", tt.minor, tt.minorUnits)
	}
}

// TestAmountRoundTrip checks that formatted amounts parse back to the same
// minor units at every supported exponent, including four decimal places.
func TestAmountRoundTrip(t *testing.T) {
	tests := []struct {
		minor      int64
		minorUnits int32
	}{
		{minor: 12345, minorUnits: 4},
		{minor: 5, minorUnits: 4},
		{minor: 1234, minorUnits: 3},
		{minor: 255, minorUnits: 2},
		{minor: 10000, minorUnits: 2},
		{minor: 123, minorUnits: 0},
	}
	for _, tt := range tests {
		formatted := FormatAmount(tt.minor, tt.minorUnits)
		parsed, err := ParseAmount(formatted, tt.minorUnits)
		require.NoError(t, err, "ParseAmount(%q, %d)", formatted, tt.minorUnits)
		assert.Equal(t, tt.minor, parsed, "round trip of %d at %d minor units", tt.minor, tt.minorUnits)
	}
}

// TestUnsupportedMinorUnitsFallBackToZero documents today's behavior: an
// exponent outside 0..4 makes the formatters return "0" instead of reporting
// the bad currency data (finding C6, deliberately unchanged by this plan).
func TestUnsupportedMinorUnitsFallBackToZero(t *testing.T) {
	for _, minorUnits := range []int32{5, -1} {
		assert.Equal(t, "0", FormatAmount(12345, minorUnits), "FormatAmount minor units %d", minorUnits)
		assert.Equal(t, "0", DisplayAmount(12345, minorUnits), "DisplayAmount minor units %d", minorUnits)
	}
}

func TestDisplayAmount(t *testing.T) {
	tests := []struct {
		minor      int64
		minorUnits int32
		want       string
	}{
		{minor: 255, minorUnits: 2, want: "2.55"},
		{minor: 123456789, minorUnits: 2, want: "1,234,567.89"},
		{minor: 123, minorUnits: 0, want: "123"},
		{minor: 100000, minorUnits: 0, want: "100,000"},
		{minor: -123456, minorUnits: 2, want: "-1,234.56"},
		{minor: 123456789, minorUnits: 4, want: "12,345.6789"},
		{minor: 5, minorUnits: 4, want: "0.0005"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, DisplayAmount(tt.minor, tt.minorUnits), "DisplayAmount(%d, %d)", tt.minor, tt.minorUnits)
	}
}
