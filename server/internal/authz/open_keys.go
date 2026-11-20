package authz

// openKeys is one scope's open keys as the registry declares them: the grant
// keys each kind of check may leave out. scopeOpenKeys holds them and
// Check.mayOmit reads them.
type openKeys struct {
	Probe          []string
	Connection     []string
	PolicyAudience []string
}
