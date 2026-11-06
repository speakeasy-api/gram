package mcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// callChangedMember runs a call through execute_tool on a gateway whose catalog approved member
// before its settings changed, or, with signInChanged, before its sign-in changed.
func callChangedMember(t *testing.T, s *Service, gate *metaGateContext, member metaMember, signInChanged bool) string {
	t.Helper()
	gate.execute = true
	gate.catalogRef = &toolfilter.CatalogReference{ID: uuid.New(), Mode: toolfilter.CatalogModeLive}
	approved := connectionCatalogMember{ServerID: member.serverID, ProjectID: member.projectID, BackendHash: "approved", Auth: nil}
	member.configurationHash = "changed"
	if member.backend == metaMemberBackendProxied {
		approved.Auth = &connectionCatalogAuth{ClientID: uuid.Nil, RegistrationID: member.chosenClientID}
		if signInChanged {
			approved.Auth.RegistrationID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}
	}
	gate.catalog = &connectionCatalog{ID: uuid.New(), Members: map[string]connectionCatalogMember{member.slug: approved}}
	body, err := s.executeMetaMemberTool(t.Context(), testenv.NewLogger(t), gate, []metaMember{member}, &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID("1"), Method: "tools/call"}, member.slug+"--tool", json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	return string(body)
}

// A call refused because its approval no longer matches names the server and records whoever its message tells to act first.
func TestCatalogConsentCallNamesTheServerAndRecordsTheRemedy(t *testing.T) {
	t.Parallel()
	reconnectable, adminOnly := uuid.New(), uuid.New()
	for name, tc := range map[string]struct {
		gate          func(s *Service) *metaGateContext
		member        uuid.UUID
		hosted        bool
		signInChanged bool
		text          string
		reason        mcpconnectionattempts.Code
		detail        consentRejectionReason
		who           mcpconnectionattempts.WhoFixes
	}{
		"consent approves the new sign-in": {
			gate: func(s *Service) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{reconnectable: true})
			},
			member: reconnectable, signInChanged: true,
			text:   `Server \"db\"'s sign-in changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"consent approves the new settings": {
			gate: func(s *Service) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{reconnectable: true})
			},
			member: reconnectable,
			text:   `Server \"db\"'s settings changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"a hosted server needs no lookup": {
			gate:   func(s *Service) *metaGateContext { return staleChoiceRemedyGate(s, nil) },
			member: uuid.New(), hosted: true,
			text:   `Server \"db\"'s settings changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"reconnect, then ask an administrator": {
			gate: func(s *Service) *metaGateContext {
				gate := staleChoiceRemedyGate(s, nil)
				gate.authentication.refreshableUserSession = false
				return gate
			},
			member: uuid.New(), signInChanged: true,
			text:   `Server \"db\"'s sign-in changed since you approved it. Reconnect this gateway in your AI app and approve it again. If that doesn't help, ask your administrator.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"consent cannot approve it": {
			gate: func(s *Service) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{adminOnly: false})
			},
			member: adminOnly, signInChanged: true,
			text:   `Server \"db\"'s sign-in changed, and you can't approve it again yourself. Ask your administrator to finish setting it up or give you access.`,
			reason: mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoAdmin,
		},
		"whoever approved the connection approves again": {
			gate: func(*Service) *metaGateContext {
				return &metaGateContext{organizationID: "org", projectID: uuid.New(), metaServerID: uuid.New(), authentication: &issuerGateAuthentication{subject: urn.NewAgentSubject(uuid.New())}}
			},
			member: uuid.New(),
			text:   `Server \"db\"'s settings changed since this connection was approved. Ask whoever set up this connection to approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			s := &Service{connections: rec.Recorder, logger: testenv.NewLogger(t), staleNoAuthLog: newOnceLog(), telemLogger: noTelemetryLogs(t)}
			member := metaMember{slug: "db", serverID: tc.member, projectID: uuid.New(), backend: metaMemberBackendProxied}
			if tc.hosted {
				member.backend = metaMemberBackendHosted
			}
			body := callChangedMember(t, s, tc.gate(s), member, tc.signInChanged)
			require.Contains(t, body, tc.text)
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.reason)
			require.Equal(t, tc.member.String(), row.MemberID)
			require.Equal(t, string(tc.detail), row.SubReason)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// Code Mode carries the slug separately, so its message leaves the server out.
func TestCatalogConsentMessageLeavesOutAnUnnamedServer(t *testing.T) {
	t.Parallel()
	require.Equal(t, "This server's sign-in changed, and you can't approve it again yourself. Ask your administrator to finish setting it up or give you access.",
		catalogConsentMessage("", consentRejectionAuthenticationChanged, staleChoiceAskAdmin, true))
}
