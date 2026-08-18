package demoseed

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
)

// Spec names the tenant a seed run provisions. The embedded SQL is written
// against DefaultSpec's literals; every other tenant is produced by rewriting
// those literals, so one script body serves the shared prod demo org, the
// local development org, and the safety test's adversarial fixture tenant.
//
// This is the same mechanism TestDemoSeedSafety has always used to provision
// its "other tenant" — lifted out of the test so production code and the test
// cannot drift apart.
//
// Every field must be a globally unique identifier family: two Specs that
// share one field would collide on a unique index, or worse, silently share
// rows. Validate enforces that they are at least pairwise distinct.
type Spec struct {
	// OrgID is the WorkOS-style organization id — the primary isolation
	// boundary for every Postgres write and most ClickHouse ones.
	OrgID string
	// OrgSlug is the organization slug (globally unique, and the prefix of
	// generated MCP slugs).
	OrgSlug string
	// OrgName is the organization's display name.
	OrgName string
	// UUIDPrefix is the first 8 hex characters of every fixed UUID the seed
	// writes (project id, policy ids, toolset ids, ...).
	UUIDPrefix string
	// UserPrefix prefixes every seeded user id, and through it the derived
	// workos_*, membership, and directory ids.
	UserPrefix string
	// EmailDomain is the seeded users' email domain. users.email is globally
	// unique.
	EmailDomain string
	// NameSeed prefixes every det_uuid()/trace-id name, so derived ids in
	// both stores differ per tenant.
	NameSeed string
	// GroupPrefix prefixes workos_directory_group_id, which is globally
	// unique.
	GroupPrefix string
	// Marker is the gram.deployment.id stamped on every telemetry row, which
	// the ClickHouse postflight leak-checks.
	Marker string
}

// DefaultSpec is the shared read-only demo organization — the tenant the
// embedded SQL is literally written against, and the one the daily production
// run provisions. Rewriting a script with this Spec is a no-op.
//
// OrgID must stay in sync with constants.DemoOrganizationID (asserted by
// TestSpecMatchesConstants).
func DefaultSpec() Spec {
	return Spec{
		OrgID:       "org_gram_demo_workspace",
		OrgSlug:     "acme-demo",
		OrgName:     "Acme Demo Org",
		UUIDPrefix:  "dec0de00",
		UserPrefix:  "user_demo_",
		EmailDomain: "@demo.getgram.ai",
		NameSeed:    "gram-demo-",
		GroupPrefix: "demo_grp_",
		Marker:      "demo-seed",
	}
}

// LocalSpec is the local development organization. Its identity comes
// straight from the dev-idp's default org, so logging in locally lands you
// inside the seeded data as a first-class member rather than as a read-only
// impersonator.
//
// Unlike DefaultSpec this tenant is writable: none of the demo carve-outs in
// authz.Engine or middleware.DemoOrgWriteGuard key off it.
func LocalSpec() Spec {
	return Spec{
		OrgID:       devidentity.DefaultOrgWorkosID,
		OrgSlug:     devidentity.DefaultOrgSlug,
		OrgName:     devidentity.DefaultOrgName,
		UUIDPrefix:  "10ca1000",
		UserPrefix:  "user_locl_",
		EmailDomain: "@local.getgram.ai",
		NameSeed:    "gram-locl-",
		GroupPrefix: "locl_grp_",
		Marker:      "local-seed",
	}
}

// ProjectID is the seed's single project, derived from UUIDPrefix.
func (s Spec) ProjectID() string {
	return s.FixedUUID("0000-4000-a000-000000000001")
}

// AssetID is the OpenAPI document asset the runner uploads the embedded spec
// to, derived from UUIDPrefix.
func (s Spec) AssetID() string {
	return s.FixedUUID("0000-4000-a000-00000000a001")
}

// DeploymentID is the completed deployment the seeded tool stack hangs off.
func (s Spec) DeploymentID() string {
	return s.FixedUUID("0000-4000-a000-00000000d001")
}

// FixedUUID composes one of the seed's fixed uuids from this tenant's prefix
// and the remaining four groups, e.g. "0000-4000-a000-000000000001".
func (s Spec) FixedUUID(rest string) string {
	return s.UUIDPrefix + "-" + rest
}

// fields returns the Spec's identifier families in the order Rewrite must
// apply them. Longer/more specific patterns come first: the backslash-escaped
// LIKE pattern must be rewritten before the plain user id prefix, or the
// postflight LIKE patterns stop matching the rewritten ids.
func (s Spec) fields() []string {
	return []string{
		escapeLike(s.UserPrefix),
		s.UserPrefix,
		s.OrgID,
		s.UUIDPrefix,
		s.NameSeed,
		s.EmailDomain,
		s.OrgSlug,
		s.GroupPrefix,
		s.Marker,
		s.OrgName,
	}
}

// Identifiers returns the Spec's identifier families — every string that, if
// found in another tenant's row, proves this tenant leaked into it.
func (s Spec) Identifiers() []string { return s.fields() }

// escapeLike mirrors how the SQL writes an identifier prefix inside a LIKE
// pattern, where '_' is a wildcard and must be escaped.
func escapeLike(v string) string {
	return strings.ReplaceAll(v, "_", `\_`)
}

// Rewrite retargets a seed script written against DefaultSpec at this Spec.
// Rewriting with DefaultSpec returns the script unchanged.
func (s Spec) Rewrite(script string) string {
	def := DefaultSpec().fields()
	for i, to := range s.fields() {
		script = strings.ReplaceAll(script, def[i], to)
	}
	return script
}

// Validate reports whether the Spec can safely coexist with the default demo
// tenant in one database. Each field must be non-empty, must not collide with
// the corresponding default field unless the whole Spec is the default, and
// must not contain any default identifier — a target that embeds a source
// would be rewritten again by a later replacement pass.
func (s Spec) Validate() error {
	def := DefaultSpec()
	defFields, fields := def.fields(), s.fields()

	if s == def {
		return nil
	}

	for i, v := range fields {
		if v == "" {
			return fmt.Errorf("demo seed spec: field %d is empty", i)
		}
		if v == defFields[i] {
			return fmt.Errorf("demo seed spec: field %q is shared with the default demo tenant; every identifier family must be distinct", v)
		}
		for _, d := range defFields {
			if strings.Contains(v, d) {
				return fmt.Errorf("demo seed spec: field %q contains the default identifier %q, which a later rewrite pass would corrupt", v, d)
			}
		}
	}

	return nil
}
