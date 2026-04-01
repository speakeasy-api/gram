package access

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadGrants loads and normalizes grants for the given organization and principals.
func LoadGrants(ctx context.Context, db accessrepo.DBTX, organizationID string, principals []urn.Principal) (*Grants, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURNs, err := parsePrincipalURNs(principals)
	if err != nil {
		return nil, err
	}

	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query principal grants: %w", err)
	}

	grantRows := make([]grantRow, 0, len(rows))
	for _, row := range rows {
		grantRows = append(grantRows, grantRow{
			Scope:    Scope(row.Scope),
			Resource: row.Resource,
		})
	}

	if len(grantRows) == 0 {
		return &Grants{rows: nil}, nil
	}

	return &Grants{rows: grantRows}, nil
}

func parsePrincipalURNs(principals []urn.Principal) ([]string, error) {
	if len(principals) == 0 {
		return nil, fmt.Errorf("no principals provided")
	}

	seen := make(map[string]struct{}, len(principals))
	principalURNs := make([]string, 0, len(principals))
	for _, principal := range principals {
		text, err := principal.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("marshal principal urn %q: %w", principal.String(), err)
		}

		principalURN := string(text)

		if _, ok := seen[principalURN]; ok {
			continue
		}

		seen[principalURN] = struct{}{}
		principalURNs = append(principalURNs, principalURN)
	}

	return principalURNs, nil
}
