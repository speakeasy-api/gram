package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/oops"
	goahttp "goa.design/goa/v3/http"
)

func TestImageUnavailableStatus(t *testing.T) {
	t.Parallel()

	for name, encode := range map[string]func(context.Context, http.ResponseWriter, error) error{
		"upload": adminserver.EncodeUploadPlatformImageError(goahttp.ResponseEncoder, nil),
		"serve":  adminserver.EncodeServeImageError(goahttp.ResponseEncoder, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			rec := httptest.NewRecorder()
			if err := encode(ctx, rec, oops.C(oops.CodeUnavailable).AsGoa(ctx)); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
		})
	}
}
