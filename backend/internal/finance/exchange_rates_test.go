package finance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalRate(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantRate    string
		wantMessage string
	}{
		{name: "blank", raw: "   ", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "not a number", raw: "abc", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "zero", raw: "0", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "zero with decimals", raw: "0.00000000", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "negative", raw: "-1.5", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "scientific notation", raw: "1e3", wantMessage: "Enter a rate with at most 8 decimal places."},
		{name: "scientific notation with negative exponent", raw: "1.5E-3", wantMessage: "Enter a rate with at most 8 decimal places."},
		{name: "nine decimal places", raw: "1.123456789", wantMessage: "Enter a rate with at most 8 decimal places."},
		{name: "trailing zero past eight decimal places", raw: "1.1234567890", wantMessage: "Enter a rate with at most 8 decimal places."},
		{name: "ten billion", raw: "10000000000", wantMessage: "Enter a smaller rate."},
		{name: "eight decimal places", raw: "1.12345678", wantRate: "1.12345678"},
		{name: "smallest eight-decimal rate", raw: "0.00000001", wantRate: "0.00000001"},
		{name: "trailing zeros canonicalize", raw: " 117.50000000 ", wantRate: "117.5"},
		{name: "nine decimal places of trailing zeros", raw: "1.230000000", wantRate: "1.23"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, message := canonicalRate(tt.raw)

			if tt.wantMessage != "" {
				require.NotNil(t, message, "validation message")
				assert.Equal(t, tt.wantMessage, message.text)
				assert.Empty(t, rate, "rate")
				return
			}
			assert.Nil(t, message, "validation message")
			assert.Equal(t, tt.wantRate, rate)
		})
	}
}

func TestRateStringTrimsInsignificantZeroes(t *testing.T) {
	tests := []struct {
		name string
		rate string
		want string
	}{
		{name: "stored scale", rate: "1.23000000", want: "1.23"},
		{name: "whole number", rate: "100", want: "100"},
		{name: "one decimal", rate: "1.10000000", want: "1.1"},
		{name: "smallest unit", rate: "0.00000001", want: "0.00000001"},
		{name: "no fractional part", rate: "117.00000000", want: "117"},
		{name: "unparseable falls back to raw", rate: "not-a-rate", want: "not-a-rate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := ExchangeRate{Rate: tt.rate}

			assert.Equal(t, tt.want, rate.RateString(), "RateString()")
		})
	}
}
