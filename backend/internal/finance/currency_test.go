package finance

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderCurrencies(t *testing.T) {
	catalog := []Currency{
		{Code: "AUD"},
		{Code: "EUR"},
		{Code: "JPY"},
		{Code: "RSD"},
		{Code: "USD"},
	}
	tests := []struct {
		name     string
		selected string
		recent   []string
		want     []string
	}{
		{
			name:     "selected first then the catalog",
			selected: "RSD",
			want:     []string{"RSD", "AUD", "EUR", "JPY", "USD"},
		},
		{
			name:     "recent currencies follow the selection",
			selected: "RSD",
			recent:   []string{"EUR", "USD"},
			want:     []string{"RSD", "EUR", "USD", "AUD", "JPY"},
		},
		{
			name:     "recent repeats collapse",
			selected: "RSD",
			recent:   []string{"EUR", "EUR", "USD"},
			want:     []string{"RSD", "EUR", "USD", "AUD", "JPY"},
		},
		{
			name:     "recent selection does not repeat",
			selected: "EUR",
			recent:   []string{"EUR", "USD"},
			want:     []string{"EUR", "USD", "AUD", "JPY", "RSD"},
		},
		{
			name:   "no selection keeps recents first",
			recent: []string{"JPY", "RSD"},
			want:   []string{"JPY", "RSD", "AUD", "EUR", "USD"},
		},
		{
			name:     "codes outside the catalog are ignored",
			selected: "XXX",
			recent:   []string{"XXX", "EUR"},
			want:     []string{"EUR", "AUD", "JPY", "RSD", "USD"},
		},
		{
			name: "empty history keeps the catalog",
			want: []string{"AUD", "EUR", "JPY", "RSD", "USD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OrderCurrencies(catalog, tt.selected, tt.recent)

			codes := make([]string, 0, len(got))
			for _, currency := range got {
				codes = append(codes, currency.Code)
			}
			assert.Equal(t, tt.want, codes)
			assert.Len(t, got, len(catalog), "ordered choices dropped a supported currency")
		})
	}
}
