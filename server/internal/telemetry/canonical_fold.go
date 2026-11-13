package telemetry

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	usersRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// queryTouchesEmail reports whether a request involves the email dimension,
// the only case where folding changes anything.
func queryTouchesEmail(groupBy string, filters []repo.AttributeMetricsFilter) bool {
	if groupBy == "email" {
		return true
	}
	for _, f := range filters {
		if f.Dimension == "email" && len(f.Values) > 0 {
			return true
		}
	}
	return false
}

// CanonicalOrgFor returns the org id identity-folded queries run under.
func (s *Service) CanonicalOrgFor(_ context.Context, orgID string) string {
	return orgID
}

// resolveUserScope resolves an employee identifier into either a canonical
// map-backed identity or the legacy Postgres-expanded set — exactly one of the
// two is populated, so the repo's identity filter (which unions whichever are
// supplied) applies just the one returned here.
func (s *Service) resolveUserScope(ctx context.Context, orgID, identifier string) (repo.UserIdentity, repo.CanonicalUserIdentity) {
	none := repo.CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""}
	if identifier == "" {
		return repo.UserIdentity{UserIDs: nil, Emails: nil}, none
	}
	if ident := s.resolveCanonicalUserIdentity(ctx, orgID, identifier); ident.Enabled() {
		return repo.UserIdentity{UserIDs: nil, Emails: nil}, ident
	}
	// The org id failed the SQL-literal allowlist, so the canonical filter
	// cannot be built for it. Degrade to the legacy expanded scope — returning
	// the disabled identity alongside the empty legacy set would leave the
	// per-user queries with no user filter at all.
	return s.resolveEmployeeIdentity(ctx, orgID, identifier), none
}

// resolveCanonicalUserIdentity builds the map-backed identity for one
// identifier. An email identifier needs no lookup at all — the owning user id
// and the folded email comparison both come from the identity_map inside the
// query. A user-id identifier keeps one directory lookup for the email leg;
// when it fails the scope still matches every user_id-keyed row, so a lookup
// failure degrades coverage rather than breaking the page.
func (s *Service) resolveCanonicalUserIdentity(ctx context.Context, orgID, identifier string) repo.CanonicalUserIdentity {
	ident := repo.CanonicalUserIdentity{OrgID: orgID, UserID: "", EmailLower: ""}
	if strings.Contains(identifier, "@") {
		ident.EmailLower = conv.NormalizeEmail(identifier)
		return ident
	}

	ident.UserID = identifier
	rows, err := usersRepo.New(s.db).GetConnectedUsersByIDs(ctx, usersRepo.GetConnectedUsersByIDsParams{
		Ids:            []string{identifier},
		OrganizationID: orgID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "resolve canonical identity directory email", attr.SlogError(err))
	}
	// The identity map excludes deleted users, so a deleted directory row's
	// email may already belong to a different active owner in the map; folding
	// it would pull that owner's email-only rows onto this page. Degrade to
	// id-only scope instead, the same fallback as a lookup failure.
	if len(rows) == 1 && !rows[0].DeletedAt.Valid {
		ident.EmailLower = conv.NormalizeEmail(rows[0].Email)
	}
	return ident
}
