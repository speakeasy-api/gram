package authz

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// PrincipalCredentialAdmission contains independently loaded policy sets from
// authoritative credential admission. Policies must not be cached across requests.
type PrincipalCredentialAdmission struct {
	OwnerUserID string
	Credential  []Grant
	Agent       []Grant
	Owner       []Grant
}

// PrincipalCredentialAdmitter supplies application-owned credential admission
// without making the generic authorization engine depend on agent policy.
type PrincipalCredentialAdmitter func(context.Context, *pgxpool.Pool) (PrincipalCredentialAdmission, error)

// AdmitPrincipalCredential fails closed unless application-owned admission is
// configured and succeeds, then preserves R, A, and O as independent policies.
func (e *Engine) AdmitPrincipalCredential(ctx context.Context) (context.Context, error) {
	if e.admitPrincipalCredential == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	admission, err := e.admitPrincipalCredential(ctx, e.db)
	if err != nil {
		return ctx, err
	}
	return applyPrincipalCredentialAdmission(ctx, admission)
}

// PrincipalCredentialDBTXAdmitter admits credentials on a caller-owned snapshot.
type PrincipalCredentialDBTXAdmitter func(context.Context, accessrepo.DBTX) (PrincipalCredentialAdmission, error)

// AdmitPrincipalCredentialWithDBTX fails closed without transaction-bound admission.
func (e *Engine) AdmitPrincipalCredentialWithDBTX(ctx context.Context, db accessrepo.DBTX) (context.Context, error) {
	if e.admitPrincipalCredentialWithDBTX == nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	admission, err := e.admitPrincipalCredentialWithDBTX(ctx, db)
	if err != nil {
		return ctx, err
	}
	return applyPrincipalCredentialAdmission(ctx, admission)
}

func applyPrincipalCredentialAdmission(ctx context.Context, admission PrincipalCredentialAdmission) (context.Context, error) {
	if admission.OwnerUserID == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, admission.OwnerUserID)
	return principalCredentialPoliciesToContext(ctx, admission.Credential, admission.Agent, admission.Owner), nil
}
