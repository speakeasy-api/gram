package activities

// highestCrossedAlertThreshold returns the highest alert threshold a usage
// percentage has crossed: 50, 75, 90, 100, then — when escalatePast100 — every
// additional 50 beyond that. Returns 0 while usage sits below the lowest
// threshold. Shared by the TUM and OpenRouter-credits alert ladders so the
// warning levels cannot drift between the two email families.
func highestCrossedAlertThreshold(pct int64, escalatePast100 bool) int64 {
	switch {
	case pct < 50:
		return 0
	case pct < 75:
		return 50
	case pct < 90:
		return 75
	case pct < 100:
		return 90
	case !escalatePast100:
		return 100
	default:
		return 100 + (pct-100)/50*50
	}
}
