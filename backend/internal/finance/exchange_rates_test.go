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
		{name: "negative", raw: "-1.5", wantMessage: "Enter a positive rate with at most 8 decimal places."},
		{name: "nine decimal places", raw: "1.123456789", wantMessage: "Enter a rate with at most 8 decimal places."},
		{name: "ten billion", raw: "10000000000", wantMessage: "Enter a smaller rate."},
		{name: "eight decimal places", raw: "1.12345678", wantRate: "1.12345678"},
		{name: "trailing zeros canonicalize", raw: " 117.50000000 ", wantRate: "117.5"},
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
