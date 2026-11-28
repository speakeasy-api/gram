package emailnorm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/emailnorm"
)

func TestCanonicalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		want      string
		wantGmail bool
	}{
		{
			name:      "plain gmail address",
			input:     "alice@gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "uppercase and surrounding whitespace",
			input:     " \t\nALICE@GMAIL.COM\r\n ",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "gmail dotted local part",
			input:     "a.l.i.c.e@gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "gmail plus tag",
			input:     "alice+test@gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "gmail empty plus tag",
			input:     "alice+@gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "gmail multiple plus tags",
			input:     "alice+first+second@gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "gmail dots and plus tag combined",
			input:     "A.l.i.c.e+work.2026@Gmail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "googlemail maps to gmail",
			input:     "alice@googlemail.com",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "googlemail dots plus tag uppercase whitespace",
			input:     "  A.L.I.C.E+tag.one@GoogleMail.COM  ",
			want:      "alice@gmail.com",
			wantGmail: true,
		},
		{
			name:      "non-gmail preserves dots and plus tag",
			input:     "  Alice.Smith+Work@Example.COM  ",
			want:      "alice.smith+work@example.com",
			wantGmail: false,
		},
		{
			name:      "subdomain of gmail is not normalized as gmail",
			input:     "a.l.i.c.e+tag@sub.gmail.com",
			want:      "a.l.i.c.e+tag@sub.gmail.com",
			wantGmail: false,
		},
		{
			name:      "similar domain suffix is not normalized as gmail",
			input:     "a.l.i.c.e+tag@notgmail.com",
			want:      "a.l.i.c.e+tag@notgmail.com",
			wantGmail: false,
		},
		{
			name:      "googlemail country domain is not normalized as gmail",
			input:     "a.l.i.c.e+tag@googlemail.co.uk",
			want:      "a.l.i.c.e+tag@googlemail.co.uk",
			wantGmail: false,
		},
		{
			name:      "empty string",
			input:     "",
			want:      "",
			wantGmail: false,
		},
		{
			name:      "whitespace only",
			input:     "   \t\n ",
			want:      "",
			wantGmail: false,
		},
		{
			name:      "missing at sign",
			input:     " Not-An-Email ",
			want:      "not-an-email",
			wantGmail: false,
		},
		{
			name:      "empty local part",
			input:     "@gmail.com",
			want:      "@gmail.com",
			wantGmail: false,
		},
		{
			name:      "empty domain part",
			input:     "Alice@",
			want:      "alice@",
			wantGmail: false,
		},
		{
			name:      "multiple at signs",
			input:     "a.b@c@gmail.com",
			want:      "a.b@c@gmail.com",
			wantGmail: false,
		},
		{
			name:      "degenerate all dots local part",
			input:     "...@gmail.com",
			want:      "...@gmail.com",
			wantGmail: false,
		},
		{
			name:      "degenerate dots with plus tag",
			input:     "...+tag@gmail.com",
			want:      "...+tag@gmail.com",
			wantGmail: false,
		},
		{
			name:      "degenerate plus tag only",
			input:     "+tag@googlemail.com",
			want:      "+tag@googlemail.com",
			wantGmail: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, emailnorm.Canonicalize(tc.input))
			require.Equal(t, tc.wantGmail, emailnorm.IsGmailDomain(tc.input))
		})
	}
}

func TestCanonicalize_DegenerateInputsDoNotCollide(t *testing.T) {
	t.Parallel()

	require.NotEqual(t,
		emailnorm.Canonicalize("...+one@gmail.com"),
		emailnorm.Canonicalize("...+two@gmail.com"),
	)
	require.NotEqual(t,
		emailnorm.Canonicalize("...@gmail.com"),
		emailnorm.Canonicalize("..@gmail.com"),
	)
}
