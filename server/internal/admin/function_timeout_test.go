package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSetFunctionTimeout_DefaultsUntilSet(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_fn_timeout", name: "Timeout Co", slug: "timeout-co", whitelisted: true})

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_fn_timeout"})
	require.NoError(t, err)
	require.Equal(t, 60, conv.PtrValOr(got.FunctionTimeoutSeconds, 0))
	require.True(t, conv.PtrValOr(got.FunctionTimeoutIsDefault, false))

	res, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
		AdminSessionToken:      nil,
		OrganizationID:         "org_fn_timeout",
		FunctionTimeoutSeconds: new(300),
	})
	require.NoError(t, err)
	require.Equal(t, 300, conv.PtrValOr(res.FunctionTimeoutSeconds, 0))
	require.False(t, conv.PtrValOr(res.FunctionTimeoutIsDefault, true))

	got, err = svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_fn_timeout"})
	require.NoError(t, err)
	require.Equal(t, 300, conv.PtrValOr(got.FunctionTimeoutSeconds, 0))
}

func TestSetFunctionTimeout_AbsentRestoresDefault(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_fn_timeout_reset", name: "Reset Co", slug: "reset-co", whitelisted: true})

	_, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
		AdminSessionToken:      nil,
		OrganizationID:         "org_fn_timeout_reset",
		FunctionTimeoutSeconds: new(45),
	})
	require.NoError(t, err)

	res, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
		AdminSessionToken:      nil,
		OrganizationID:         "org_fn_timeout_reset",
		FunctionTimeoutSeconds: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 60, conv.PtrValOr(res.FunctionTimeoutSeconds, 0))
	require.True(t, conv.PtrValOr(res.FunctionTimeoutIsDefault, false))
}

func TestSetFunctionTimeout_AcceptsBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_fn_timeout_bounds", name: "Bounds Co", slug: "bounds-co", whitelisted: true})

	for _, seconds := range []int{30, 600} {
		res, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
			AdminSessionToken:      nil,
			OrganizationID:         "org_fn_timeout_bounds",
			FunctionTimeoutSeconds: new(seconds),
		})
		require.NoError(t, err)
		require.Equal(t, seconds, conv.PtrValOr(res.FunctionTimeoutSeconds, 0))
	}
}

func TestSetFunctionTimeout_RejectsOutOfRange(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_fn_timeout_bad", name: "Bad Timeout Co", slug: "bad-timeout-co", whitelisted: true})

	for _, seconds := range []int{-1, 0, 29, 601, 3600} {
		_, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
			AdminSessionToken:      nil,
			OrganizationID:         "org_fn_timeout_bad",
			FunctionTimeoutSeconds: new(seconds),
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_fn_timeout_bad"})
	require.NoError(t, err)
	require.True(t, conv.PtrValOr(got.FunctionTimeoutIsDefault, false), "a rejected value must not be stored")
}

func TestSetFunctionTimeout_UnknownOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.SetFunctionTimeout(ctx, &gen.SetFunctionTimeoutPayload{
		AdminSessionToken:      nil,
		OrganizationID:         "org_missing",
		FunctionTimeoutSeconds: new(120),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
