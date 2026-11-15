package finance

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDescription(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		want        string
		wantMessage string
	}{
		{name: "blank is optional", raw: "   "},
		{name: "trims whitespace", raw: "  lunch ", want: "lunch"},
		{name: "invalid utf-8", raw: "lunch\xff", wantMessage: "Description contains invalid characters."},
		{name: "null byte", raw: "lunch\x00", wantMessage: "Description contains invalid characters."},
		{name: "five hundred characters", raw: strings.Repeat("a", 500), want: strings.Repeat("a", 500)},
		{name: "five hundred and one characters", raw: strings.Repeat("a", 501), wantMessage: "Description must be 500 characters or fewer."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			description, message := normalizeDescription(tt.raw)

			if tt.wantMessage != "" {
				require.NotNil(t, message, "validation message")
				assert.Equal(t, tt.wantMessage, message.text)
				assert.Nil(t, description, "description")
				return
			}
			assert.Nil(t, message, "validation message")
			if tt.want == "" {
				assert.Nil(t, description, "description")
				return
			}
			require.NotNil(t, description, "description")
			assert.Equal(t, tt.want, *description)
		})
	}
}
