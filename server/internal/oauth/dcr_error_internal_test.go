package oauth

import "testing"

func TestDCRErrorDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		statusCode int
		want       string
	}{
		{
			name:       "error and description",
			body:       `{"error":"invalid_client_metadata","error_description":"redirect_uris must be loopback"}`,
			statusCode: 400,
			want:       "invalid_client_metadata: redirect_uris must be loopback",
		},
		{
			name:       "description only",
			body:       `{"error_description":"scope not supported"}`,
			statusCode: 400,
			want:       "scope not supported",
		},
		{
			name:       "error only",
			body:       `{"error":"invalid_client_metadata"}`,
			statusCode: 422,
			want:       "invalid_client_metadata",
		},
		{
			name:       "empty body falls back to status",
			body:       ``,
			statusCode: 403,
			want:       "HTTP 403",
		},
		{
			name:       "non-json body falls back to status",
			body:       `<html>forbidden</html>`,
			statusCode: 403,
			want:       "HTTP 403",
		},
		{
			name:       "json without error fields falls back to status",
			body:       `{"foo":"bar"}`,
			statusCode: 400,
			want:       "HTTP 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := dcrErrorDetail([]byte(tt.body), tt.statusCode)
			if got != tt.want {
				t.Fatalf("dcrErrorDetail(%q, %d) = %q, want %q", tt.body, tt.statusCode, got, tt.want)
			}
		})
	}
}
