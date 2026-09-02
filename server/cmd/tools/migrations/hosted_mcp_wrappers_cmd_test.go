package main

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/hostedmcpbackfill"
)

func hostedMCPWrappersGetenv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://test"
	}
	return ""
}

func TestParseHostedMCPWrappersFlagsDefaultsToDryRun(t *testing.T) {
	t.Parallel()

	cfg, err := parseHostedMCPWrappersFlags(nil, hostedMCPWrappersGetenv)
	require.NoError(t, err)
	require.False(t, cfg.options.Apply)
	require.Equal(t, hostedmcpbackfill.PhaseWrappers, cfg.options.Phase)
	require.Empty(t, cfg.options.Aliases)
}

func TestParseHostedMCPWrappersFlagsParsesScopeAndAllowlist(t *testing.T) {
	t.Parallel()

	project, cursor, domain := uuid.New(), uuid.New(), uuid.New()
	cfg, err := parseHostedMCPWrappersFlags([]string{
		"-apply", "-acknowledge-mirror-deployed", "-move-dependents", "-project=" + project.String(), "-cursor=" + cursor.String(),
		"-limit=5", "-aliases= a-slug@" + domain.String() + " ,", "-report=/tmp/r.json",
	}, hostedMCPWrappersGetenv)
	require.NoError(t, err)
	require.True(t, cfg.options.Apply)
	require.Equal(t, hostedmcpbackfill.PhaseDependents, cfg.options.Phase)
	require.Equal(t, uuid.NullUUID{UUID: project, Valid: true}, cfg.options.ProjectID)
	require.Equal(t, cursor, cfg.options.Cursor)
	require.Equal(t, 5, cfg.options.Limit)
	require.Equal(t, []hostedmcpbackfill.AliasKey{{Slug: "a-slug", CustomDomainID: domain}}, cfg.options.Aliases)
	require.Equal(t, "/tmp/r.json", cfg.reportPath)

	cfg, err = parseHostedMCPWrappersFlags([]string{"-retire-toolset-grants"}, hostedMCPWrappersGetenv)
	require.NoError(t, err)
	require.Equal(t, hostedmcpbackfill.PhaseRetireGrants, cfg.options.Phase)
}

func TestParseHostedMCPWrappersFlagsRejectsBadInput(t *testing.T) {
	t.Parallel()

	_, err := parseHostedMCPWrappersFlags(nil, func(string) string { return "" })
	require.ErrorContains(t, err, "GRAM_DATABASE_URL")
	_, err = parseHostedMCPWrappersFlags([]string{"-apply"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "acknowledge-mirror-deployed")
	_, err = parseHostedMCPWrappersFlags([]string{"-move-dependents", "-retire-toolset-grants"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "choose one")
	_, err = parseHostedMCPWrappersFlags([]string{"-project=nope"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "invalid -project")
	_, err = parseHostedMCPWrappersFlags([]string{"-cursor=nope"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "invalid -cursor")
	_, err = parseHostedMCPWrappersFlags([]string{"-aliases=bare-slug"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "slug@custom_domain_id")
	_, err = parseHostedMCPWrappersFlags([]string{"extra"}, hostedMCPWrappersGetenv)
	require.ErrorContains(t, err, "positional")
}
