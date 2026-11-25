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

	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProblemText_EachRule(t *testing.T) {
	t.Parallel()
	linear, github, notion, linearCopy := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	n := names{
		servers: map[string][]repo.ListServerNamesRow{
			linear.String():     {{ID: linear, Name: "Linear", Kind: "mcp"}},
			github.String():     {{ID: github, Name: "GitHub", Kind: "mcp"}},
			notion.String():     {{ID: notion, Name: "Notion", Kind: "mcp"}},
			linearCopy.String(): {{ID: linearCopy, Name: "Linear", Kind: "mcp"}},
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
	token := func(subject string, server uuid.UUID, reason mcpconnectionattempts.Code) mcpconnectionattempts.FailingPerson {
		e := ep(subject, server, mcpconnectionattempts.PartAppSignIn, reason)
		e.Step = mcpconnectionattempts.StepInboundToken.String()
		return e
	}
	notConnected := mcpconnectionattempts.CodeInvalidRemoteSession
	opened := func(e mcpconnectionattempts.FailingPerson, reason mcpconnectionattempts.Code) mcpconnectionattempts.FailingPerson {
		e.OpeningReason = reason.String()
		return e
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
				SharedApp: app, OtherAppsWorked: true},
			headline: "3 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodeInvalidBearerToken) + " All use Claude Code 2.1.4. Another app worked on this server in this time.",
		},
		{
			name: "same service the provider fixes",
			problem: problems.Problem{Rule: problems.RuleSameService, Scope: problems.Scope{Kind: problems.ScopeService, Key: "mcp.linear.app"}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:b", github, server, mcpconnectionattempts.CodeProviderUnavailable)}},
			headline: "2 people can't connect to mcp.linear.app",
			fact:     "Requests to servers using mcp.linear.app are failing.",
		},
		{
			name: "a server that answers other apps",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 2, OtherAppsWorked: true,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:b", linear, server, mcpconnectionattempts.CodeProviderServerError)}},
			headline: "2 people can't connect to Linear",
			fact:     admin(mcpconnectionattempts.CodeProviderServerError) + " Another app worked on this server in this time.",
		},
		{
			name: "one recorded error on two servers",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:dana", github, server, mcpconnectionattempts.CodeProviderServerError)}},
			headline: "Dana Reyes can't connect to 2 MCP servers",
			fact:     "On Linear and GitHub. Requests to these servers are failing.",
		},
		{
			name: "requests Speakeasy has to fix",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: server, WhoFixes: mcpconnectionattempts.WhoSpeakeasy, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeAuthCheckUnavailable), ep("user:b", linear, server, mcpconnectionattempts.CodeTunnelUnavailable)}},
			headline: "2 people can't connect to Linear",
			fact:     "Requests to this server are failing.",
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
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsWorked: true},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On Linear and GitHub. The app's sign-in to Speakeasy fails. Another app worked on each of these servers in this time.",
		},
		{
			name: "same app version on one server",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsWorked: true},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On Linear. The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodeInvalidBearerToken) + " Another app worked on this server in this time.",
		},
		{
			name: "same app version on one server and before reaching a server",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken), func() mcpconnectionattempts.FailingPerson {
					e := ep("user:b", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken)
					e.ServerKind, e.ServerID = "authorization_server", "https://auth.example.com"
					return e
				}()},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsWorked: true},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On Linear, and before reaching an MCP server. The app's sign-in to Speakeasy fails. Another app worked on this server in this time.",
		},
		{
			name: "one person with a rejected saved key on one server",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: service, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, service, mcpconnectionattempts.CodeStaticKeyRejected)}},
			headline: "Dana Reyes can't connect to Linear",
			fact:     "On Linear. " + admin(mcpconnectionattempts.CodeStaticKeyRejected),
		},
		{
			name: "one person with a rejected saved key on two servers",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: service, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, service, mcpconnectionattempts.CodeStaticKeyRejected), ep("user:dana", github, service, mcpconnectionattempts.CodeStaticKeyRejected)}},
			headline: "Dana Reyes can't connect to 2 MCP servers",
			fact:     "On Linear and GitHub. Requests with the saved sign-in for the service are failing.",
		},
		{
			name: "a sign-in check that failed on two servers",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: server, WhoFixes: mcpconnectionattempts.WhoSpeakeasy, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, server, mcpconnectionattempts.CodeAuthCheckUnavailable), ep("user:dana", github, server, mcpconnectionattempts.CodeAuthCheckUnavailable)}},
			headline: "Dana Reyes can't connect to 2 MCP servers",
			fact:     "On Linear and GitHub. Requests to these servers are failing.",
		},
		{
			name: "same service where another app worked",
			problem: problems.Problem{Rule: problems.RuleSameService, Scope: problems.Scope{Kind: problems.ScopeService, Key: "mcp.linear.app"}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 2, OtherAppsWorked: true,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, server, mcpconnectionattempts.CodeProviderServerError), ep("user:b", github, server, mcpconnectionattempts.CodeProviderServerError)}},
			headline: "2 people can't connect to mcp.linear.app",
			fact:     "Requests to servers using mcp.linear.app are failing. Another app worked on at least one of the servers using mcp.linear.app in this time.",
		},
		{
			name: "mixed saved credentials including a misconfiguration",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: service, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, service, mcpconnectionattempts.CodeStaticKeyRejected), ep("user:dana", linear, service, mcpconnectionattempts.CodeCredentialMisconfigured)}},
			headline: "Dana Reyes can't connect to Linear",
			fact:     "On Linear. Requests with the saved sign-in for the service are failing.",
		},
		{
			name: "same server where nobody has connected their account",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 7,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), token("user:b", linear, notConnected)},
				SharedApp: app, OtherAppsWorked: true},
			headline: "7 people need to connect an account for Linear",
			fact:     "All use Claude Code 2.1.4. For each person, the connection their app asked for on Linear is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "several people each on a different server",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), token("user:b", github, notConnected)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}, OtherAppsWorked: true},
			headline: "2 people need to connect accounts for Linear and GitHub",
			fact:     "All use Claude Code 2.0.1. For each person, the connection their app asked for on at least one of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "people whose servers overlap",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), token("user:a", github, notConnected), token("user:b", github, notConnected)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}},
			headline: "2 people need to connect accounts for Linear and GitHub",
			fact:     "All use Claude Code 2.0.1. For each person, the connection their app asked for on at least one of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "an app version where nobody has connected their account names the server",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), token("user:b", linear, notConnected)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}},
			headline: "2 people need to connect an account for Linear",
			fact:     "All use Claude Code 2.0.1. For each person, the connection their app asked for on Linear is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "several people on three servers",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 3,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), token("user:b", github, notConnected), token("user:c", notion, notConnected)},
				SharedApp: problems.App{Family: mcpconnectionattempts.FamilyClaudeCode, Version: "2.0.1"}},
			headline: "3 people need to connect accounts for Linear and 2 other MCP servers",
			fact:     "All use Claude Code 2.0.1. For each person, the connection their app asked for on at least one of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "two servers with no name",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:a", uuid.New(), notConnected), token("user:b", uuid.New(), notConnected)}},
			headline: "2 people need to connect accounts for 2 MCP servers",
			fact:     "For each person, the connection their app asked for on at least one of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "two servers where one has a name",
			problem: problems.Problem{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyClaudeCode + "@2.0.1"}, Part: signIn, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken), ep("user:b", uuid.New(), signIn, mcpconnectionattempts.CodeInvalidBearerToken)}},
			headline: "2 people on Claude Code 2.0.1 can't sign in",
			fact:     "On Linear and 1 other MCP server. The app's sign-in to Speakeasy fails.",
		},
		{
			name: "two servers with the same name",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:dana", linear, notConnected), token("user:dana", linearCopy, notConnected)}},
			headline: "Dana Reyes needs to connect accounts for 2 MCP servers",
			fact:     "The connection their app asked for on each of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "three servers where two share a name",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:dana", linear, notConnected), token("user:dana", linearCopy, notConnected), token("user:dana", github, notConnected)}},
			headline: "Dana Reyes needs to connect accounts for 3 MCP servers",
			fact:     "The connection their app asked for on each of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "one person who has not connected their account",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 1,
				Episodes:  []mcpconnectionattempts.FailingPerson{token("user:dana", linear, notConnected)},
				SharedApp: app},
			headline: "Dana Reyes needs to connect an account for Linear",
			fact:     "They use Claude Code 2.1.4. The connection their app asked for on Linear is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "one person on two servers",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:dana", linear, notConnected), token("user:dana", github, notConnected)}},
			headline: "Dana Reyes needs to connect accounts for Linear and GitHub",
			fact:     "The connection their app asked for on each of these servers is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "only the latest reason decides",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{opened(token("user:a", linear, notConnected), mcpconnectionattempts.CodeInvalidBearerToken), opened(token("user:b", linear, notConnected), mcpconnectionattempts.CodeInvalidBearerToken)}},
			headline: "2 people need to connect an account for Linear",
			fact:     "For each person, the connection their app asked for on Linear is missing, has expired, or can no longer be used. If their app shows a connection link, they can use it to connect or reconnect.",
		},
		{
			name: "an opening connect reason with another latest reason keeps today's wording",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{opened(token("user:a", linear, mcpconnectionattempts.CodeInvalidBearerToken), notConnected), opened(token("user:b", linear, mcpconnectionattempts.CodeInvalidBearerToken), notConnected)}},
			headline: "2 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodeInvalidBearerToken),
		},
		{
			name: "a connect run that reached no MCP server keeps today's wording",
			problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 1,
				Episodes: []mcpconnectionattempts.FailingPerson{func() mcpconnectionattempts.FailingPerson {
					e := token("user:dana", linear, notConnected)
					e.ServerKind, e.ServerID = "authorization_server", "https://auth.example.com"
					return e
				}()}},
			headline: "Dana Reyes can't sign in",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(notConnected),
		},
		{
			name: "some people have not connected their account and others must sign in again",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:a", linear, notConnected), ep("user:b", linear, signIn, mcpconnectionattempts.CodeInvalidBearerToken)}},
			headline: "2 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails.",
		},
		{
			name: "no usable connection recorded off the token step",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoPerson, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{ep("user:a", linear, signIn, notConnected), ep("user:b", linear, signIn, notConnected)}},
			headline: "2 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(notConnected),
		},
		{
			name: "sign-in not set up on the token step",
			problem: problems.Problem{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: linear.String()}, Part: signIn, WhoFixes: mcpconnectionattempts.WhoAdmin, People: 2,
				Episodes: []mcpconnectionattempts.FailingPerson{token("user:a", linear, mcpconnectionattempts.CodeUpstreamSignInNotSetUp), token("user:b", linear, mcpconnectionattempts.CodeUpstreamSignInNotSetUp)}},
			headline: "2 people can't sign in to Linear",
			fact:     "The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodeUpstreamSignInNotSetUp),
		},
	}
	cases = append(cases, struct {
		name     string
		problem  problems.Problem
		headline string
		fact     string
	}{
		name: "one person signing in on no server",
		problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: signIn, People: 1,
			Episodes: []mcpconnectionattempts.FailingPerson{{Episode: mcpconnectionattempts.Episode{SubjectURN: "user:dana", ServerKind: "authorization_server", ServerID: "issuer", Part: signIn, Reason: mcpconnectionattempts.CodePKCEMismatch.String(), OpeningReason: mcpconnectionattempts.CodePKCEMismatch.String()}}}},
		headline: "Dana Reyes can't sign in",
		fact:     "The app's sign-in to Speakeasy fails. " + admin(mcpconnectionattempts.CodePKCEMismatch),
	})
	viaGateway := ep("user:dana", uuid.New(), server, mcpconnectionattempts.CodeProviderServerError)
	viaGateway.ServerKind, viaGateway.MemberID = "meta", linear.String()
	cases = append(cases, struct {
		name     string
		problem  problems.Problem
		headline string
		fact     string
	}{
		name: "one person on one server directly and through a gateway",
		problem: problems.Problem{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: "user:dana"}, Part: server, WhoFixes: mcpconnectionattempts.WhoProvider, People: 1,
			Episodes: []mcpconnectionattempts.FailingPerson{ep("user:dana", linear, server, mcpconnectionattempts.CodeProviderServerError), viaGateway}},
		headline: "Dana Reyes can't connect to Linear",
		fact:     "On Linear. " + admin(mcpconnectionattempts.CodeProviderServerError),
	})
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
			require.NotContains(t, view.Fact, "not answering", "a recorded failure is not proof the server is down")
			for _, second := range []string{"your", "Your", "We ", "You "} {
				require.NotContains(t, view.Fact, second, "the fact line never speaks to the person")
			}
			require.NotContains(t, view.Fact, "not accepted")
			if needsOwnConnection(tc.problem) {
				require.NotContains(t, view.Headline, "can't sign in", "a person who has not connected their account is not failing to sign in")
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
	shown, total := capProblems(all, maxProblems)
	require.Len(t, shown, maxProblems)
	require.Equal(t, int64(maxProblems+1), total)
	require.Equal(t, "0", shown[0].ID, "the most people first stay")
}

func TestCapProblems_PutsWhatEachPersonFixesLast(t *testing.T) {
	t.Parallel()
	all := []problems.Problem{
		{ID: "person-7", WhoFixes: mcpconnectionattempts.WhoPerson, People: 7},
		{ID: "admin-3", WhoFixes: mcpconnectionattempts.WhoAdmin, People: 3},
		{ID: "person-2", WhoFixes: mcpconnectionattempts.WhoPerson, People: 2},
		{ID: "provider-2", WhoFixes: mcpconnectionattempts.WhoProvider, People: 2},
		{ID: "speakeasy-1", WhoFixes: mcpconnectionattempts.WhoSpeakeasy, People: 1},
		{ID: "unknown-1", WhoFixes: mcpconnectionattempts.WhoNobody, People: 1},
	}
	shown, total := capProblems(all, maxProblems)
	require.Equal(t, int64(6), total)
	ids := make([]string, len(shown))
	for i, p := range shown {
		ids[i] = p.ID
	}
	require.Equal(t, []string{"admin-3", "provider-2", "speakeasy-1", "unknown-1", "person-7", "person-2"}, ids, "the grouping's order stays inside each group")
}

func TestCapProblems_CapsAfterOrdering(t *testing.T) {
	t.Parallel()
	all := make([]problems.Problem, maxProblems+1)
	for i := range all {
		all[i].ID, all[i].WhoFixes = strconv.Itoa(i), mcpconnectionattempts.WhoPerson
	}
	all[maxProblems].WhoFixes = mcpconnectionattempts.WhoAdmin
	shown, total := capProblems(all, maxProblems)
	require.Len(t, shown, maxProblems)
	require.Equal(t, int64(maxProblems+1), total)
	require.Equal(t, strconv.Itoa(maxProblems), shown[0].ID, "an admin's problem past the cap is listed first")
	require.Equal(t, strconv.Itoa(maxProblems-2), shown[maxProblems-1].ID, "the last person's problem is cut")
}

func TestWhoFixesOf_NamesEveryParty(t *testing.T) {
	t.Parallel()
	require.Equal(t, gen.SuspectedProblemWhoFixes("unknown"), whoFixesOf(mcpconnectionattempts.WhoNobody))
	for _, w := range []mcpconnectionattempts.WhoFixes{mcpconnectionattempts.WhoPerson, mcpconnectionattempts.WhoAdmin, mcpconnectionattempts.WhoProvider, mcpconnectionattempts.WhoApp, mcpconnectionattempts.WhoSpeakeasy} {
		require.Equal(t, gen.SuspectedProblemWhoFixes(w), whoFixesOf(w))
	}
}

func TestPastDeadline_IsTemporarilyUnavailableButKeepsRefusals(t *testing.T) {
	t.Parallel()
	s := &Service{logger: testenv.NewLogger(t)}
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	var shareable *oops.ShareableError
	require.ErrorAs(t, s.pastDeadline(expired, fmt.Errorf("read: %w", context.DeadlineExceeded)), &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.ErrorAs(t, s.pastDeadline(expired, oops.E(oops.CodeUnexpected, errors.New("boom"), "load names")), &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code, "any read past the budget")
	require.ErrorAs(t, s.pastDeadline(expired, oops.C(oops.CodeForbidden)), &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.ErrorAs(t, s.pastDeadline(t.Context(), oops.E(oops.CodeUnexpected, errors.New("boom"), "load names")), &shareable)
	require.Equal(t, oops.CodeUnexpected, shareable.Code, "inside the budget nothing changes")
}
