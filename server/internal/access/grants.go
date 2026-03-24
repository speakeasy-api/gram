package access

const WildcardResource = "*"

// Grants is the in-memory grant set resolved for a request.
type Grants struct {
	rows []grantRow
}

type grantRow struct {
	Scope    Scope
	Resource string
}

func (g *Grants) hasAccess(scope Scope, resourceID string) bool {
	if g == nil {
		return false
	}

	for _, row := range g.rows {
		if row.Scope != scope {
			continue
		}

		if row.Resource == WildcardResource || row.Resource == resourceID {
			return true
		}
	}

	return false
}
