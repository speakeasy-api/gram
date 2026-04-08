package access

// Check describes a single scope/resource authorization requirement.
type Check struct {
	Scope      Scope
	ResourceID string
}

func validateCheck(check Check) error {
	switch check.ResourceID {
	case "":
		return InvalidCheck(check.Scope, check.ResourceID)
	case WildcardResource:
		return InvalidCheck(check.Scope, check.ResourceID)
	default:
		return nil
	}
}
