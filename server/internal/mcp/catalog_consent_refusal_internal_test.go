package mcp

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// approveChangedMember gives gate a catalog that approved member before its settings changed,
// or, with signInChanged, before its sign-in changed.
func approveChangedMember(gate *metaGateContext, member *metaMember, mode toolfilter.CatalogMode, signInChanged bool) {
	gate.catalogRef = &toolfilter.CatalogReference{ID: uuid.New(), Mode: mode}
	approved := connectionCatalogMember{ServerID: member.serverID, ProjectID: member.projectID, BackendHash: "approved", Auth: nil}
	member.configurationHash = "changed"
	if member.backend == metaMemberBackendProxied {
		approved.Auth = &connectionCatalogAuth{ClientID: uuid.Nil, RegistrationID: member.chosenClientID}
		if signInChanged {
			approved.Auth.RegistrationID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}
	}
	gate.catalog = &connectionCatalog{ID: uuid.New(), Members: map[string]connectionCatalogMember{member.slug: approved}}
}

// callChangedMember runs a call through execute_tool on a gateway whose catalog approved member before it changed.
func callChangedMember(t *testing.T, s *Service, gate *metaGateContext, member metaMember, signInChanged bool) string {
	t.Helper()
	gate.execute = true
	approveChangedMember(gate, &member, toolfilter.CatalogModeLive, signInChanged)
	body, err := s.executeMetaMemberTool(t.Context(), testenv.NewLogger(t), gate, []metaMember{member}, &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID("1"), Method: "tools/call"}, member.slug+"--tool", json.RawMessage(`{}`), nil)
	require.NoError(t, err)
	return string(body)
}

// catalogConsentRemedyCases are the remedy forms a changed approval can take.
func catalogConsentRemedyCases() map[string]struct {
	gate          func(s *Service, member uuid.UUID) *metaGateContext
	hosted        bool
	signInChanged bool
	remedy        staleChoiceRemedy
	text          string
	reason        mcpconnectionattempts.Code
	detail        consentRejectionReason
	who           mcpconnectionattempts.WhoFixes
} {
	return map[string]struct {
		gate          func(s *Service, member uuid.UUID) *metaGateContext
		hosted        bool
		signInChanged bool
		remedy        staleChoiceRemedy
		text          string
		reason        mcpconnectionattempts.Code
		detail        consentRejectionReason
		who           mcpconnectionattempts.WhoFixes
	}{
		"consent approves the new sign-in": {
			gate: func(s *Service, member uuid.UUID) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{member: true})
			},
			signInChanged: true, remedy: staleChoiceReconnect,
			text:   `The sign-in for the "db" server changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"consent approves the new settings": {
			gate: func(s *Service, member uuid.UUID) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{member: true})
			},
			remedy: staleChoiceReconnect,
			text:   `The settings for the "db" server changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"a hosted server needs no lookup": {
			gate:   func(s *Service, _ uuid.UUID) *metaGateContext { return staleChoiceRemedyGate(s, nil) },
			hosted: true, remedy: staleChoiceReconnect,
			text:   `The settings for the "db" server changed since you approved it. Reconnect this gateway in your AI app and approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"reconnect, then ask an administrator": {
			gate: func(s *Service, _ uuid.UUID) *metaGateContext {
				gate := staleChoiceRemedyGate(s, nil)
				gate.authentication.refreshableUserSession = false
				return gate
			},
			signInChanged: true, remedy: staleChoiceReconnectThenAdmin,
			text:   `The sign-in for the "db" server changed since you approved it. Reconnect this gateway in your AI app and approve it again. If that doesn't help, ask your administrator.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoPerson,
		},
		"consent cannot approve it": {
			gate: func(s *Service, member uuid.UUID) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{member: false})
			},
			signInChanged: true, remedy: staleChoiceAskAdmin,
			text:   `The sign-in for the "db" server changed, and you can't approve it again yourself. Ask your administrator to finish setting it up or give you access.`,
			reason: mcpconnectionattempts.CodeApprovalNeedsAdmin, detail: consentRejectionAuthenticationChanged, who: mcpconnectionattempts.WhoAdmin,
		},
		"consent cannot approve the new settings": {
			gate: func(s *Service, member uuid.UUID) *metaGateContext {
				return staleChoiceRemedyGate(s, map[uuid.UUID]bool{member: false})
			},
			remedy: staleChoiceAskAdmin,
			text:   `The settings for the "db" server changed, and you can't approve it again yourself. Ask your administrator to finish setting it up or give you access.`,
			reason: mcpconnectionattempts.CodeApprovalNeedsAdmin, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoAdmin,
		},
		"whoever approved the connection approves again": {
			gate: func(*Service, uuid.UUID) *metaGateContext {
				return &metaGateContext{organizationID: "org", projectID: uuid.New(), metaServerID: uuid.New(), authentication: &issuerGateAuthentication{subject: urn.NewAgentSubject(uuid.New())}}
			},
			remedy: staleChoiceReconnect,
			text:   `The settings for the "db" server changed since this connection was approved. Ask whoever set up this connection to approve it again.`,
			reason: mcpconnectionattempts.CodeConsentNotApproved, detail: consentRejectionConfigurationChanged, who: mcpconnectionattempts.WhoPerson,
		},
	}
}

func changedMember(serverID uuid.UUID, hosted bool) metaMember {
	member := metaMember{slug: "db", serverID: serverID, projectID: uuid.New(), backend: metaMemberBackendProxied}
	if hosted {
		member.backend = metaMemberBackendHosted
	}
	return member
}

// A call refused because its approval no longer matches names the server and records whoever its message tells to act first.
func TestCatalogConsentCallNamesTheServerAndRecordsTheRemedy(t *testing.T) {
	t.Parallel()
	for name, tc := range catalogConsentRemedyCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			s := &Service{connections: rec.Recorder, logger: testenv.NewLogger(t), staleNoAuthLog: newOnceLog(), telemLogger: noTelemetryLogs(t)}
			serverID := uuid.New()
			body := callChangedMember(t, s, tc.gate(s, serverID), changedMember(serverID, tc.hosted), tc.signInChanged)
			want, err := json.Marshal(tc.text)
			require.NoError(t, err)
			require.Contains(t, body, string(want[1:len(want)-1]))
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.reason)
			require.Equal(t, serverID.String(), row.MemberID)
			require.Equal(t, string(tc.detail), row.SubReason)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// Describing a changed member, its connection status and its Code Mode helpers give the call's remedy, and record nothing.
func TestCatalogConsentDescribeAndStatusFollowTheCall(t *testing.T) {
	t.Parallel()
	for name, tc := range catalogConsentRemedyCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := mcpconnectionattemptstest.New(t)
			s := &Service{connections: rec.Recorder, logger: testenv.NewLogger(t)}
			serverID := uuid.New()
			member := changedMember(serverID, tc.hosted)
			gate := tc.gate(s, serverID)
			gate.execute = true
			approveChangedMember(gate, &member, toolfilter.CatalogModeLive, tc.signInChanged)
			logger := testenv.NewLogger(t)
			err := s.validateCatalogMember(t.Context(), gate, member)
			require.Equal(t, tc.text, s.memberFailureMessage(t.Context(), logger, gate, member, err))

			unnamed := catalogConsentMessage("", tc.detail, tc.remedy, gate.authentication.subject.Kind == urn.SessionSubjectKindUser)
			refusal := (&metaCodeBackend{service: s}).helperFailure(t.Context(), gate, member, err)
			if tc.remedy == staleChoiceReconnect {
				require.Equal(t, codemode.ErrConsentRequired, refusal)
			} else {
				refused, ok := errors.AsType[*codemode.MemberRefusalError](refusal)
				require.True(t, ok)
				require.Equal(t, unnamed, refused.Message)
				require.Equal(t, tc.remedy != staleChoiceAskAdmin, refused.ConsentRequired)
			}

			if tc.signInChanged {
				// A cached status reads the approval without checking the sign-in, so it sees only settings changes.
				return
			}
			gate.execute = false
			gate.catalogRef.Mode = toolfilter.CatalogModeCached
			status, detail, outage := s.memberSignInStatus(t.Context(), logger, gate, member, nil)
			require.Nil(t, outage)
			require.Equal(t, unnamed, detail)
			wantStatus := metamcp.SignInNeedsConsent
			if tc.remedy == staleChoiceAskAdmin {
				wantStatus = metamcp.SignInActionRequired
			}
			require.Equal(t, wantStatus, status)
			require.Empty(t, rec.Rows())
		})
	}
}

// In Code Mode, a call reconnecting can't fix keeps the slug out of its message and is not a consent request.
func TestCatalogConsentCodeModeCallNeedsAnAdministrator(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	s := &Service{connections: rec.Recorder, logger: testenv.NewLogger(t)}
	serverID := uuid.New()
	member := changedMember(serverID, false)
	gate := staleChoiceRemedyGate(s, map[uuid.UUID]bool{serverID: false})
	gate.execute = true
	approveChangedMember(gate, &member, toolfilter.CatalogModeLive, true)
	consent, ok := errors.AsType[catalogConsentError](s.validateCatalogMember(t.Context(), gate, member))
	require.True(t, ok)

	refusal := catalogConsentCodeRefusal(s.refuseCatalogConsentCall(t.Context(), s.logger, gate, member, consent, false))
	refused, ok := errors.AsType[*codemode.MemberRefusalError](refusal)
	require.True(t, ok)
	require.Equal(t, "The sign-in for this server changed, and you can't approve it again yourself. Ask your administrator to finish setting it up or give you access.", refused.Message)
	require.False(t, refused.ConsentRequired)
	require.NotErrorIs(t, refusal, codemode.ErrConsentRequired)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeApprovalNeedsAdmin)
	require.Equal(t, string(consentRejectionAuthenticationChanged), row.SubReason)
}

// When we can't check whether consent would fix it, the person reconnects first, and the row blames our lookup.
func TestCatalogConsentFailedLookupRecordsOurError(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_catalog_consent_lookup")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.staleChoiceConsent = expirable.NewLRU[string, bool](10, nil, time.Minute)
	pool.Close()
	rec := mcpconnectionattemptstest.New(t)
	f.service.connections = rec.Recorder
	f.service.telemLogger = noTelemetryLogs(t)
	subject := urn.NewUserSubject("user_1")
	gate := &metaGateContext{
		organizationID: f.orgID, projectID: f.projectID, metaServerID: uuid.New(), userSessionIssuerID: uuid.New(),
		authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: subject},
		memberChoices:  &memberChoices{selector: &upstreamSelector{}, subject: subject, byServer: map[uuid.UUID]*memberTokens{}},
	}
	member := metaMember{slug: "db", serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied}

	body := callChangedMember(t, f.service, gate, member, true)
	require.Contains(t, body, `The sign-in for the \"db\" server changed since you approved it. Reconnect this gateway in your AI app and approve it again. If that doesn't help, ask your administrator.`)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, string(consentRejectionAuthenticationChanged), row.SubReason)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
	require.Zero(t, f.service.staleChoiceConsent.Len(), "a retry must look again")
}

// Under enforcement, a changed member that needs no upstream sign-in is fixed by a new consent, not an administrator.
func TestCatalogConsentNoSignInMemberReconnectsUnderEnforcement(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_catalog_consent_no_auth")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.features = new(feature.InMemory)
	f.service.staleChoiceConsent = expirable.NewLRU[string, bool](10, nil, time.Minute)
	f.enforce()
	rec := mcpconnectionattemptstest.New(t)
	f.service.connections = rec.Recorder
	f.service.staleNoAuthLog = newOnceLog()
	f.service.telemLogger = noTelemetryLogs(t)
	subject := urn.NewUserSubject("user_1")
	gate := &metaGateContext{
		organizationID: f.orgID, projectID: f.projectID, metaServerID: uuid.New(), userSessionIssuerID: uuid.New(),
		authentication: &issuerGateAuthentication{refreshableUserSession: true, subject: subject},
		memberChoices:  &memberChoices{selector: &upstreamSelector{}, subject: subject, byServer: map[uuid.UUID]*memberTokens{}},
	}
	member := metaMember{slug: "db", serverID: f.serverID, projectID: f.projectID, backend: metaMemberBackendProxied}

	body := callChangedMember(t, f.service, gate, member, false)
	require.Contains(t, body, `The settings for the \"db\" server changed since you approved it. Reconnect this gateway in your AI app and approve it again.`)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeConsentNotApproved)
	require.Equal(t, string(consentRejectionConfigurationChanged), row.SubReason)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}
