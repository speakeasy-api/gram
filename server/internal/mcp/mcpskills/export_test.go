package mcpskills

// SetServedSkillLimit lowers the number of skills one resolution serves, so a
// test can reach the cap with a handful of skills.
func SetServedSkillLimit(r *Resolver, limit int32) {
	r.limit = limit
}
