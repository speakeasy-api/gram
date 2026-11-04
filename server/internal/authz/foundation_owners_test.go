package authz

import (
	"path"
	"strings"
)

// Backend Revamp tickets that own a migration (Linear project "Practical
// RBAC", milestone "Backend Revamp", parent AIS-830). The keys match the
// ticket work keys used in docs/rbac-foundation-migration.md.
const (
	ownerA1  = "A1 AIS-879"
	ownerAG1 = "AG1 AIS-856"
	ownerAG2 = "AG2 AIS-857"
	ownerAS1 = "AS1 AIS-860"
	ownerC1  = "C1 AIS-869"
	ownerC3  = "C3 AIS-872"
	ownerE1  = "E1 AIS-852"
	ownerF5  = "F5 AIS-835"
	ownerG1  = "G1 AIS-850"
	ownerG2  = "G2 AIS-849"
	ownerI1  = "I1 AIS-864"
	ownerL1  = "L1 AIS-867"
	ownerM1  = "M1 AIS-859"
	ownerM2  = "M2 AIS-865"
	ownerM3  = "M3 AIS-861"
	ownerM4  = "M4 AIS-866"
	ownerM5  = "M5 AIS-863"
	ownerM6  = "M6 AIS-862"
	ownerO1  = "O1 AIS-837"
	ownerO2  = "O2 AIS-838"
	ownerO3  = "O3 AIS-839"
	ownerO4  = "O4 AIS-844"
	ownerO5  = "O5 AIS-840"
	ownerO6  = "O6 AIS-842"
	ownerO7  = "O7 AIS-841"
	ownerO8  = "O8 AIS-845"
	ownerP1  = "P1 AIS-843"
	ownerP2  = "P2 AIS-851"
	ownerP3  = "P3 AIS-847"
	ownerP4  = "P4 AIS-846"
	ownerP5  = "P5 AIS-848"
	ownerPL1 = "PL1 AIS-855"
	ownerR1  = "R1 AIS-868"
	ownerS1  = "S1 AIS-853"
	ownerS2  = "S2 AIS-854"
	ownerW1  = "W1 AIS-858"
	ownerX1  = "X1 AIS-873"
	ownerX2  = "X2 AIS-874"
	ownerX3  = "X3 AIS-875"
	ownerX4  = "X4 AIS-876"
)

// foundationPackageOwners assigns each consumer package to the ticket whose
// code entry points name it. foundationFileOwners and foundationFunctionOwners
// split packages that more than one ticket migrates.
var foundationPackageOwners = map[string]string{
	"cmd/directory-roles":                 ownerF5,
	"cmd/gram":                            ownerF5,
	"internal/access":                     ownerO2,
	"internal/admin":                      ownerX1,
	"internal/agentmanagement":            ownerAG1,
	"internal/agents/runtimepolicy":       ownerX2,
	"internal/assistantidentity":          ownerAS1,
	"internal/assistants":                 ownerAS1,
	"internal/authclients":                ownerM5,
	"internal/background/activities":      ownerX1,
	"internal/chat":                       ownerC1,
	"internal/demoseed":                   ownerX4,
	"internal/keys":                       ownerAG2,
	"internal/launcher":                   ownerX3,
	"internal/mcp":                        ownerM3,
	"internal/mcpapproval":                ownerR1,
	"internal/mcpservers/upstreamclients": ownerM5,
	"internal/mcpservers/upstreamclients/authclientbackfill": ownerM5,
	"internal/platformmcp":          ownerX3,
	"internal/plugins":              ownerPL1,
	"internal/plugins/assignments":  ownerPL1,
	"internal/plugins/roledelivery": ownerM6,
	"internal/productfeatures":      ownerO1,
	"internal/risk":                 ownerR1,
	"internal/risk/approval":        ownerR1,
	"internal/risk/policybypass":    ownerR1,
	"internal/risk/policycore":      ownerR1,
	"internal/risk/policylifecycle": ownerR1,
	"internal/shadowmcp/admission":  ownerR1,
	"internal/skills":               ownerS1,
	"internal/telemetry/analytics":  ownerL1,
	"internal/variations":           ownerP3,
	"internal/workloadpolicy":       ownerW1,
}

var foundationFileOwners = map[string]string{
	"internal/agent/connections.go":                 ownerM6,
	"internal/agent/pluginservers.go":               ownerM6,
	"internal/agentmanagement/connection_blocks.go": ownerAG2,
	"internal/agentmanagement/delegable.go":         ownerAG2,
	"internal/agentmanagement/policy.go":            ownerAG2,
	"internal/chat/conversation_access.go":          ownerC3,
	"internal/mcp/authclientresolve.go":             ownerM5,
	"internal/mcp/mandatory_shadow.go":              ownerM5,
	"internal/billingnotifications/recipients.go":   ownerX1,
	"internal/mcp/serve_platform.go":                ownerX3,
	"internal/plugins/delivery_principals.go":       ownerS2,
	"internal/plugins/role_matches.go":              ownerS2,
	"internal/skillefficacy":                        ownerO7,
	// The dataset access check outlives the analytics service's migration
	// until the dashboards, widgets and Platform MCP callers move to
	// AccessOperation.
	"internal/telemetry/analytics/catalog.go": ownerP4,
}

var foundationFunctionOwners = map[string]string{
	"internal/auth/principalcredential/principalcredential.go *Issuer.checkAuthorizer": ownerX1,
	"internal/assistantidentity/actor.go canDelegateInference":                         ownerI1,
	"internal/access/impl.go *Service.ListGrants":                                      ownerX4,
	"internal/access/impl.go *Service.assignChallengeRole":                             ownerX1,
	"internal/access/impl.go listRoleGrantsFromGrants":                                 ownerX4,
	"internal/skillefficacy/impl.go *Service.QueryInsights":                            ownerC1,
	"internal/skillefficacy/impl.go *Service.requireProjectAccess":                     ownerAS1,
	"internal/skills/impl.go *Service.accessibleSkillIDs":                              ownerS2,
	"internal/agent":                ownerO8,
	"internal/billingnotifications": ownerO6,
	"internal/skillefficacy":        ownerO7,
	"internal/assistantidentity/provision.go *Service.selectAgent":                  ownerAG1,
	"internal/background/activities/process_workos_org_events.go handleRoleDeleted": ownerO2,
	"internal/variations/impl.go *Service.CreateGlobal":                             ownerP3,
}

// foundationOwner returns the ticket that migrates consumer. Scope metadata
// projections and stored-grant seeding move with X1's catalogue, validation
// and default projections (X2 for runtime-policy and key policies), wherever
// they are called from. Engine wiring (construction, cache invalidation,
// challenge logging) belongs to F5's shared engine construction, except in
// demoseed, which builds its own engine to seed data and stays with X4.
// Everything else follows its function, file or package.
func foundationOwner(c inventoryConsumer) string {
	dir := path.Dir(c.File)
	switch {
	case c.Category == categoryProjection && (dir == "internal/agents/runtimepolicy" || dir == "internal/keys"):
		return ownerX2
	case c.Category == categoryProjection:
		return ownerX1
	case c.Category == categoryWriter && strings.HasPrefix(c.Entry, "Seed"):
		return ownerX1
	case dir == "internal/access" && (c.Category == categoryLoader || c.Category == categoryWriter):
		return ownerX1
	case c.File == "internal/background/activities/process_workos_org_events.go" && c.Category == categoryWriter:
		return ownerX1
	case c.Category == categoryWiring && dir != "internal/demoseed":
		return ownerF5
	}
	if owner, ok := foundationFunctionOwners[c.File+" "+c.Function]; ok {
		return owner
	}
	if owner, ok := foundationFileOwners[c.File]; ok {
		return owner
	}
	return foundationPackageOwners[dir]
}
