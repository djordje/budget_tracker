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
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, FormatAmount(tt.minor, tt.minorUnits), "FormatAmount(%d, %d)", tt.minor, tt.minorUnits)
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
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, DisplayAmount(tt.minor, tt.minorUnits), "DisplayAmount(%d, %d)", tt.minor, tt.minorUnits)
	}
}
