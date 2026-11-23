package mcpconnectionhealth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	admin := func(code mcpconnectionattempts.Code) string {
		meaning, _ := mcpconnectionattempts.MeaningOf(code.String())
		return meaning.WhatHappenedForAdmin
	}
	app := problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.1.4"}
	signIn, service, server := mcpconnectionattempts.PartAppSignIn, mcpconnectionattempts.PartServiceSignIn, mcpconnectionattempts.PartServer
	cases := []struct {
		name     string
		problem  problems.Problem
		headline string
		fact     string
	}{
		{
			name: "same server, one server and one reason",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, People: 3,
				Episodes:  []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken)},
				SharedApp: app, OtherAppsServed: true},
			headline: "3 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodeInvalidBearerToken) + " All use Claude Code 2.1.4. Other apps worked there in this time.",
		},
		{
			name: "same service the provider fixes",
			problem: problems.Problem{Rule: problems.RuleSameService, Scope: problems.Scope{Kind: problems.ScopeService, Key: "mcp.linear.app"}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:b", github, server, mcpconnectionattempts.CodeProviderUnavailable)}},
			headline: "2 people can't connect to mcp.linear.app",
			fact:     "The service at mcp.linear.app is not answering.",
		},
		{
			name: "a server that answers other apps",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 2, OtherAppsServed: true,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:b", linear, server, mcpconnectionattempts.CodeProviderServerError)}},
			headline: "2 people can't connect to Linear",
			fact:     "The server does not answer these people. " + admin(mcpconnectionattempts.CodeProviderServerError) + " Other apps worked there in this time.",
		},
		{
			name: "requests Speakeasy has to fix",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: server, WhoFixes: mcpconnectionattempts.WhoSpeakeasy, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeAuthCheckUnavailable), ep("user:b", linear, server, mcpconnectionattempts.CodeTunnelUnavailable)}},
			headline: "2 people can't connect to Linear",
			fact:     "Requests to the server are failing.",
		},
		{
			name: "a service sign-in",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: service, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, service, mcpconnectionattempts.CodeInvalidGrant), ep("user:b", linear, service, mcpconnectionattempts.CodeProviderOAuthInvalidGrant)}},
			headline: "2 people can't connect to Linear",
			fact:     "Signing in to the service fails.",
		},
		{
			name: "same app version on two servers",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", github, signIn, mcpconnectionattempts.CodeInvalidBearerToken)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsServed: true},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On Linear and GitHub. The app's sign-in to Speakeasy fails. Other apps worked there in this time.",
		},
		{
			name: "one person with a rejected saved key on one server",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: service, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, service, mcpconnectionattempts.CodeStaticKeyRejected)}},
			headline: "Dana Reyes can't connect",
			fact:     "On Linear. " + admin(mcpconnectionattempts.CodeStaticKeyRejected),
		},
		{
			name: "one person with a rejected saved key on two servers",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: service, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, service, mcpconnectionattempts.CodeStaticKeyRejected), ep("user:dana", github, service, mcpconnectionattempts.CodeStaticKeyRejected)}},
			headline: "Dana Reyes can't connect",
			fact:     "On Linear and GitHub. The server rejected the saved key.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.problem.Since, tc.problem.LastFailedAt = time.Now(), time.Now()
			view, _ := problemView(n, tc.problem, "")
			require.Equal(t, tc.headline, view.Headline)
			require.Equal(t, tc.fact, view.Fact)
			for _, code := range mcpconnectionattempts.Codes() {
				require.NotContains(t, strings.Fields(view.Headline), code, "no reason codes in headlines")
			}
			if tc.problem.OtherAppsServed {
				require.NotContains(t, view.Fact, "is not answering", "never with other apps working there")
			}
			if allSavedCredential(tc.problem) {
				require.NotContains(t, view.Fact, "Signing in", "nobody signs in with a saved key")
			}
		})
	}
}

func TestCapProblems_KeepsTheFirstHundred(t *testing.T) {
	t.Parallel()
	all := make([]problems.Problem, maxProblems+1)
	for i := range all {
		all[i].ID = strconv.Itoa(i)
	}
	shown, total := capProblems(all)
	require.Len(t, shown, maxProblems)
	require.Equal(t, int64(maxProblems+1), total)
	require.Equal(t, "0", shown[0].ID, "the most people first stay")
}

func TestProblemsError_ADeadlineIsTemporarilyUnavailable(t *testing.T) {
	t.Parallel()
	s := &Service{logger: testenv.NewLogger(t)}
	var shareable *oops.ShareableError
	require.ErrorAs(t, s.problemsError(t.Context(), fmt.Errorf("read: %w", context.DeadlineExceeded), "read"), &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.ErrorAs(t, s.problemsError(t.Context(), errors.New("boom"), "read"), &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
}
