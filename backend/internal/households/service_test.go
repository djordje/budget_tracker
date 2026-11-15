package households

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		want        string
		wantMessage string
	}{
		{name: "trimmed", raw: "  Home  ", want: "Home"},
		{name: "required", raw: " \t ", wantMessage: "Household name is required."},
		{name: "invalid UTF-8", raw: "B\xffd", wantMessage: "Household name contains invalid characters."},
		{name: "control character", raw: "Home\x00", wantMessage: "Household name contains invalid characters."},
		{name: "100 runes", raw: strings.Repeat("č", 100), want: strings.Repeat("č", 100)},
		{name: "101 runes", raw: strings.Repeat("č", 101), wantMessage: "Household name must be 100 characters or fewer."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateName(tt.raw)

			if tt.wantMessage != "" {
				var validation *ValidationError
				require.ErrorAs(t, err, &validation, "validateName() error")
				assert.Equal(t, "name", validation.Field, "validation field")
				assert.Equal(t, tt.wantMessage, validation.Message, "validation message")
				assert.Empty(t, got, "validated name")
				return
			}
			require.NoError(t, err, "validateName() error")
			assert.Equal(t, tt.want, got)
		})
	}
}
