package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCredentialAdmissionHookFailsClosed(t *testing.T) {
	t.Parallel()
	admissionError := errors.New("admission failed")
	tests := map[string]PrincipalCredentialAdmitter{
		"unconfigured": nil,
		"missing owner": func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, nil
		},
		"failed admission": func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, admissionError
		},
	}
	for name, admit := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredential: admit})
			_, err := engine.AdmitPrincipalCredential(t.Context())
			if name == "failed admission" {
				require.ErrorIs(t, err, admissionError)
				return
			}
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, oops.CodeUnauthorized, denied.Code)
		})
	}
}

func TestCredentialAdmissionHookReloadsAndConjoinsPolicies(t *testing.T) {
	t.Parallel()
	calls := 0
	grants := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	admit := func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error) {
		calls++
		agent := grants
		if calls > 1 {
			agent = nil
		}
		return PrincipalCredentialAdmission{OwnerUserID: "current-owner", Credential: grants, Agent: agent, Owner: grants}, nil
	}
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredential: admit})
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"), contextvalues.PrincipalCredential{})
	prepared, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, "current-owner", owner)
	prepared, err = engine.PrepareContext(prepared)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "even a prepared context must repeat admission")
	require.Error(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}), "empty live agent policy must not inherit credential or owner authority")
}

func TestCredentialDBTXAdmissionHookFailsClosed(t *testing.T) {
	t.Parallel()
	admissionError := errors.New("admission failed")
	tests := map[string]PrincipalCredentialDBTXAdmitter{
		"unconfigured": nil,
		"missing owner": func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, nil
		},
		"failed admission": func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
			return PrincipalCredentialAdmission{}, admissionError
		},
	}
	for name, admit := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredentialWithDBTX: admit})
			_, err := engine.AdmitPrincipalCredentialWithDBTX(t.Context(), nil)
			if name == "failed admission" {
				require.ErrorIs(t, err, admissionError)
				return
			}
			var denied *oops.ShareableError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, oops.CodeUnauthorized, denied.Code)
		})
	}
}

func TestCredentialDBTXAdmissionHookReloadsAndConjoinsPolicies(t *testing.T) {
	t.Parallel()
	calls := 0
	grants := []Grant{NewGrant(ScopeProjectRead, "project-one")}
	admit := func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error) {
		calls++
		agent := grants
		if calls > 1 {
			agent = nil
		}
		return PrincipalCredentialAdmission{OwnerUserID: "current-owner", Credential: grants, Agent: agent, Owner: grants}, nil
	}
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), EngineOpts{AdmitPrincipalCredentialWithDBTX: admit})
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"), contextvalues.PrincipalCredential{})
	prepared, err := engine.AdmitPrincipalCredentialWithDBTX(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	_, owner, ok := contextvalues.PrincipalCredentialProvenance(prepared)
	require.True(t, ok)
	require.Equal(t, "current-owner", owner)
	prepared, err = engine.AdmitPrincipalCredentialWithDBTX(prepared, nil)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "even a prepared context must repeat admission")
	require.Error(t, engine.Require(prepared, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}), "empty live agent policy must not inherit credential or owner authority")
}
