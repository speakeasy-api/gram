package supportmatrix

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The embedded file is the matrix; this is the gate a pull request has to pass.
func TestEmbeddedMatrixIsValid(t *testing.T) {
	t.Parallel()

	matrix, err := Load()
	require.NoError(t, err)
	require.Len(t, matrix.Revision, 64)
	require.NotEmpty(t, matrix.Capabilities)
	require.NotEmpty(t, matrix.Platforms)
	require.NotEmpty(t, matrix.Methods)
	require.NotEmpty(t, matrix.Plans)
	for _, plan := range matrix.Plans {
		_, ok := matrix.Plan(plan.ID)
		require.True(t, ok, plan.ID)
	}
	for _, method := range matrix.Methods {
		require.Len(t, method.Platforms, len(matrix.Platforms), method.ID)
		require.Len(t, method.Claims, len(matrix.Capabilities), method.ID)
		for _, support := range method.Platforms {
			if support.Applicability == Applicable {
				require.Len(t, support.Cells, len(matrix.Capabilities), "%s on %s", method.ID, support.Platform)
			} else {
				require.Empty(t, support.Cells, "%s on %s", method.ID, support.Platform)
			}
		}
	}

	cached, err := Current()
	require.NoError(t, err)
	require.Equal(t, matrix.Revision, cached.Revision)
}

const tiny = `
capabilities:
  - { id: session, name: Session tracking, group: Observe }
platforms:
  - { id: cli, name: CLI, vendor: Vendor, family: Agent, surface: CLI }
  - { id: web, name: Web, vendor: Vendor, family: Agent, surface: Web }
methods:
  - id: hooks
    name: Hooks
    vendor: Vendor
    plans: Team plans
    claims:
      session: { status: supported, note: via hooks, verify: false }
    platforms:
      - platform: cli
        applicability: applicable
        accounts: { personal: unsupported, team: supported, enterprise: unknown }
        os: { mac: supported, linux: verify }
        note: cost only
        cells:
          session: { status: partial, note: sessions only, verify: true }
      - platform: web
        applicability: na
        accounts: { personal: unsupported, team: unsupported, enterprise: unsupported }
`

func TestParseAndLookups(t *testing.T) {
	t.Parallel()

	matrix, err := Parse([]byte(tiny))
	require.NoError(t, err)
	hooks, ok := matrix.Method("hooks")
	require.True(t, ok)
	cli, ok := hooks.Support("cli")
	require.True(t, ok)
	require.Equal(t, Fact{Status: StatusPartial, Note: "sessions only", Verify: true}, cli.Cell("session"))
	require.Equal(t, StatusPartial, cli.CellFor("session", AccountTeam).Status, "eligible accounts see the cell")
	require.Equal(t, StatusImpossible, cli.CellFor("session", AccountPersonal).Status, "ineligible accounts cannot have it")
	unknown := cli.CellFor("session", AccountEnterprise)
	require.Equal(t, StatusUnknown, unknown.Status)
	require.True(t, unknown.Verify, "unknown eligibility is left to verify")
	require.Equal(t, OSVerify, cli.OS.Linux)
	require.Empty(t, cli.OS.Windows)

	web, ok := hooks.Support("web")
	require.True(t, ok)
	require.Equal(t, StatusNA, web.Cell("session").Status, "a method that does not apply is not applicable everywhere")
	require.Equal(t, StatusNA, web.CellFor("session", AccountPersonal).Status, "even for an ineligible account")
	_, ok = hooks.Support("mobile")
	require.False(t, ok)
	_, ok = matrix.Capability("session")
	require.True(t, ok)
	_, ok = matrix.Platform("cli")
	require.True(t, ok)
}

func TestParseRejectsMistakes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ edit, want string }{
		"unknown field":                   {edit: "    plans: Team plans\n    extra: field\n", want: "field extra not found"},
		"duplicate capability":            {edit: "capabilities:\n  - { id: session, name: Twice, group: Observe }\n", want: "listed twice"},
		"plan of no platform vendor":      {edit: "plans:\n  - { id: plan, vendor: Nobody, name: Plan }\ncapabilities:\n", want: "sells no platform"},
		"platform missing":                {edit: "      - platform: web\n        applicability: na\n        accounts: { personal: unsupported, team: unsupported, enterprise: unsupported }\n", want: "lists 1 platforms, the matrix has 2"},
		"unknown platform":                {edit: "      - platform: web\n", want: "unknown platform"},
		"cells on a non-applicable entry": {edit: "        applicability: na\n", want: "cells are listed although the method does not apply"},
		"missing cell":                    {edit: "        cells:\n          session: { status: partial, note: sessions only, verify: true }\n", want: "0 capabilities listed, the matrix has 1"},
		"bad status":                      {edit: "status: partial, note: sessions only", want: "is not a status"},
		"partial without a note":          {edit: "note: sessions only, verify: true", want: "needs a note"},
		"bad applicability":               {edit: "applicability: applicable\n", want: "not applicable, na or unknown"},
		"bad eligibility":                 {edit: "personal: unsupported, team: supported, enterprise: unknown", want: "not supported, unsupported or unknown"},
		"bad os":                          {edit: "linux: verify", want: "not supported or verify"},
		"email in a note":                 {edit: "note: via hooks", want: "must not contain an email address or an id"},
		"bad slug":                        {edit: "  - id: hooks\n", want: "must be lower-case words joined by dashes"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := tiny
			switch name {
			case "unknown field":
				doc = strings.Replace(doc, "    plans: Team plans\n", tc.edit, 1)
			case "duplicate capability":
				doc = strings.Replace(doc, "capabilities:\n", tc.edit, 1)
			case "plan of no platform vendor":
				doc = strings.Replace(doc, "capabilities:\n", tc.edit, 1)
			case "platform missing":
				doc = strings.Replace(doc, tc.edit, "", 1)
			case "unknown platform":
				doc = strings.Replace(doc, tc.edit, "      - platform: mobile\n", 1)
			case "cells on a non-applicable entry":
				doc = strings.Replace(doc, "        applicability: applicable\n", tc.edit, 1)
			case "missing cell":
				doc = strings.Replace(doc, tc.edit, "        cells: {}\n", 1)
			case "bad status":
				doc = strings.Replace(doc, tc.edit, "status: maybe, note: sessions only", 1)
			case "partial without a note":
				doc = strings.Replace(doc, tc.edit, "note: '', verify: true", 1)
			case "bad applicability":
				doc = strings.Replace(doc, tc.edit, "applicability: yes\n", 1)
			case "bad eligibility":
				doc = strings.Replace(doc, tc.edit, "personal: no, team: supported, enterprise: unknown", 1)
			case "bad os":
				doc = strings.Replace(doc, tc.edit, "linux: yes", 1)
			case "email in a note":
				doc = strings.Replace(doc, tc.edit, "note: ask someone@example.com", 1)
			case "bad slug":
				doc = strings.Replace(doc, tc.edit, "  - id: Hooks_v2\n", 1)
			}
			require.NotEqual(t, tiny, doc, "the edit must change the document")
			_, err := Parse([]byte(doc))
			require.ErrorContains(t, err, tc.want)
		})
	}
}
