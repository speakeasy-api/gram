package mcpconnectionhealth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
)

func TestProblemText_EachRule(t *testing.T) {
	t.Parallel()
	linear, github := uuid.New(), uuid.New()
	n := names{
		servers: map[string][]repo.ListServerNamesRow{
			linear.String(): {{ID: linear, Name: "Linear", Kind: "mcp"}},
			github.String(): {{ID: github, Name: "GitHub", Kind: "mcp"}},
		},
		people: map[string]person{"dana": {name: "Dana Reyes", email: ""}},
	}
	ep := func(subject string, server uuid.UUID, part mcpconnectionattempts.Part, reason mcpconnectionattempts.Code) mcpconnectionattempts.FailingPerson {
		return mcpconnectionattempts.FailingPerson{Episode: mcpconnectionattempts.Episode{
			SubjectURN: subject, ServerKind: "mcp", ServerID: server.String(), Part: part, Reason: reason.String(), OpeningReason: reason.String(),
		}}
	}
	cases := []struct {
		name     string
		problem  problems.Problem
		headline string
		fact     string
	}{
		{
			name: "same server",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: mcpconnectionattempts.PartAppSignIn, People: 3,
				Episodes:  []mcpconnectionattempts.FailingPerson{ep("user:a", linear, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", linear, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.1.4"}, OtherAppsServed: true},
			headline: "3 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. This server could not accept the sign-in the app presented. All use Claude Code 2.1.4. Other apps worked there in this time.",
		},
		{
			name: "same service",
			problem: problems.Problem{Rule: problems.RuleSameService, Scope: problems.Scope{Kind: problems.ScopeService, Key: "mcp.linear.app"}, Part: mcpconnectionattempts.PartServer, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, mcpconnectionattempts.PartServer, mcpconnectionattempts.CodeProviderServerError), ep("user:b", github, mcpconnectionattempts.PartServer, mcpconnectionattempts.CodeProviderUnavailable)}},
			headline: "2 people can't connect to mcp.linear.app",
			fact:     "The service at mcp.linear.app is not answering.",
		},
		{
			name: "same app version",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: mcpconnectionattempts.PartAppSignIn, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{ep("user:a", github, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", github, mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.CodeInvalidBearerToken)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsServed: true},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On GitHub. The app's sign-in to Speakeasy fails. This server could not accept the sign-in the app presented. Other apps worked there in this time.",
		},
		{
			name: "one person",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: mcpconnectionattempts.PartServiceSignIn, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.CodeStaticKeyRejected)}},
			headline: "Dana Reyes can't connect",
			fact:     "On Linear. Signing in to the service fails. The server, or the API one of its tools calls, rejected the credential saved for it.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.problem.Since, tc.problem.LastFailedAt = time.Now(), time.Now()
			view := problemView(n, tc.problem, "")
			require.Equal(t, tc.headline, view.Headline)
			require.Equal(t, tc.fact, view.Fact)
			for _, code := range mcpconnectionattempts.Codes() {
				require.NotContains(t, strings.Fields(view.Headline), code, "no reason codes in headlines")
			}
		})
	}
}
