package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisteredTemplates_HaveUniqueKeys(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, RegisteredTemplates)
	seen := make(map[TemplateKey]Template, len(RegisteredTemplates))
	for _, tmpl := range RegisteredTemplates {
		key := tmpl.Key()
		require.NotEmpty(t, key, "template %T must have a key", tmpl)
		existing, duplicate := seen[key]
		require.Falsef(t, duplicate, "templates %T and %T share key %q", existing, tmpl, key)
		seen[key] = tmpl
	}
}

func TestParseTemplateIDs_ValidatesCompleteRegistry(t *testing.T) {
	t.Parallel()

	ids, err := ParseTemplateIDs(`{
		"team_invite":"id-1",
		"enterprise_admin_onboarding":"id-2",
		"tum_usage_threshold":"id-3",
		"tum_usage_overage":"id-4",
		"openrouter_chat_credits_threshold":"id-5",
		"openrouter_internal_credits_threshold":"id-6",
		"custom_domain_unhealthy":"id-7",
		"weekly_usage_summary":"id-8",
		"access_request":"id-9",
		"trial_ending_soon":"id-10",
		"access_paused":"id-11",
		"payg_activated":"id-12"
	}`)
	require.NoError(t, err)
	require.NoError(t, ids.ValidateRegistered())
	require.Equal(t, TransactionalID("id-1"), ids[TemplateKeyTeamInvite])
}

func TestTemplateIDs_ValidateRegisteredRejectsMissingAndUnknownKeys(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, make(TemplateIDs).ValidateRegistered(), ErrEmptyTemplateIDs)

	ids := make(TemplateIDs, len(RegisteredTemplates))
	for _, tmpl := range RegisteredTemplates {
		ids[tmpl.Key()] = TransactionalID("id-" + string(tmpl.Key()))
	}
	ids[TemplateKeyTeamInvite] = ""
	err := ids.ValidateRegistered()
	require.Error(t, err)
	require.Contains(t, err.Error(), `no Loops ID for template "team_invite"`)

	ids[TemplateKeyTeamInvite] = "id-team-invite"
	ids["unknown"] = "id-unknown"
	require.ErrorIs(t, ids.ValidateRegistered(), ErrUnknownTemplateKey)
}

func TestTeamInvite_MetadataAndVariables(t *testing.T) {
	t.Parallel()

	tmpl := TeamInvite{
		InviteLink:       "https://app.gram.sh/invite?token=abc",
		InviterName:      "Alice",
		InviterEmail:     "alice@example.com",
		OrganizationName: "Example Inc",
	}

	require.Equal(t, TemplateKeyTeamInvite, tmpl.Key())
	require.Equal(t, map[string]string{
		"invite_link":       "https://app.gram.sh/invite?token=abc",
		"inviter_name":      "Alice",
		"inviter_email":     "alice@example.com",
		"organization_name": "Example Inc",
	}, tmpl.Variables())
	require.True(t, tmpl.AddToAudience())
}

func TestParseTemplateIDs_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseTemplateIDs(`{`)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrEmptyTemplateIDs)
}
