package hostedinference

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

type fakePrincipalAdapter struct {
	active bool
	err    error
}

func (f fakePrincipalAdapter) Kind() killswitches.PrincipalKind { return PrincipalKindUser }
func (f fakePrincipalAdapter) Canonicalize(killswitches.OrganizationID, string) (killswitches.CanonicalizationResult[killswitches.PrincipalKey], error) {
	panic("unused")
}
func (f fakePrincipalAdapter) ValidateCurrentOrganization(context.Context, killswitches.OrganizationID, killswitches.PrincipalKey) (bool, error) {
	return f.active, f.err
}
func (f fakePrincipalAdapter) DeriveCandidates(_ context.Context, org killswitches.OrganizationID, source any) (killswitches.PrincipalCandidateResult, error) {
	if f.err != nil {
		return killswitches.PrincipalCandidateResult{}, f.err
	}
	provenance, ok := source.(contextvalues.ActingUserProvenance)
	if !ok || provenance.OrganizationID() != string(org) {
		return killswitches.PrincipalCandidateResult{}, errors.New("invalid provenance")
	}
	if !f.active {
		return killswitches.UnsupportedPrincipalCandidateResult(), nil
	}
	return killswitches.NewPrincipalCandidateResult([]killswitches.PrincipalCandidate{{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(provenance.UserID())}}) //nolint:wrapcheck // Preserve the constructor's exact test result.
}

type fakeEvaluator struct {
	result killswitches.EvaluationResult
	calls  int
}

func (f *fakeEvaluator) Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult {
	f.calls++
	return f.result
}

func testCheckpoint(t *testing.T, principal fakePrincipalAdapter, result killswitches.EvaluationResult) (*Checkpoint, *fakeEvaluator) {
	t.Helper()
	evaluation := &fakeEvaluator{result: result}
	return &Checkpoint{principal: principal, resource: ResourceAdapter{}, evaluator: evaluation, transport: killswitches.ResolveTransportDisposition, failurePolicy: killswitches.FailurePolicyFailClosed, timeout: time.Second}, evaluation
}

func governedContext(t *testing.T, org, user string) context.Context {
	t.Helper()
	sessionID := "session"
	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: org, UserID: user, SessionID: &sessionID}, false)
	ctx, err := WithGovernedUser(ctx, CallCategoryUserChatCompletion)
	require.NoError(t, err)
	return ctx
}

func governedChatContext(t *testing.T, org, user string) context.Context {
	t.Helper()
	ctx := contextvalues.WithValidatedChatSessionActingUser(t.Context(), org, user, "session")
	ctx, err := WithGovernedUser(ctx, CallCategoryUserChatCompletion)
	require.NoError(t, err)
	return ctx
}

func TestCheckpointDistinguishesMatchedDenialAndInfrastructure(t *testing.T) {
	t.Parallel()
	match, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", "  Paused exactly.  ")
	require.NoError(t, err)
	checkpoint, _ := testCheckpoint(t, fakePrincipalAdapter{active: true}, match)

	err = checkpoint.Check(governedContext(t, "org", "user"), "org")
	var denial *MatchedDenialError
	require.ErrorAs(t, err, &denial)
	require.Equal(t, "hosted inference access denied", denial.Error())
	require.Equal(t, "Paused exactly.", denial.ExternalNote())

	failure, err := killswitches.NewInfrastructureFailureResult(errors.New("database secret detail"))
	require.NoError(t, err)
	checkpoint, _ = testCheckpoint(t, fakePrincipalAdapter{active: true}, failure)
	err = checkpoint.Check(governedContext(t, "org", "user"), "org")
	var unavailable *InfrastructureUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, "hosted inference access evaluation is unavailable", err.Error())
	require.NotContains(t, err.Error(), "Paused")
	require.NotContains(t, err.Error(), "match")
}

func TestGovernedUserSurfaceCategoriesEvaluate(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	sessionID := "session"
	base := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID}, false)
	for _, category := range []CallCategory{
		CallCategoryUserChatCompletion,
		CallCategoryChatSummary,
		CallCategoryToolCallSummary,
		CallCategoryRiskAuthoring,
		CallCategoryBusinessMemorySearchEmbedding,
	} {
		ctx, classifyErr := WithGovernedUser(base, category)
		require.NoError(t, classifyErr)
		checkpoint, evaluation := testCheckpoint(t, fakePrincipalAdapter{active: true}, noMatch)
		require.NoError(t, checkpoint.Check(ctx, "org"))
		require.Equal(t, 1, evaluation.calls, category)
	}
}

func TestCheckpointClassificationAndIdentityPosture(t *testing.T) {
	t.Parallel()
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)

	for name, build := range map[string]func(*testing.T) (context.Context, string, fakePrincipalAdapter){
		"missing classification": func(t *testing.T) (context.Context, string, fakePrincipalAdapter) {
			t.Helper()
			return t.Context(), "org", fakePrincipalAdapter{active: true}
		},
		"cross tenant": func(t *testing.T) (context.Context, string, fakePrincipalAdapter) {
			t.Helper()
			return governedContext(t, "org-a", "user"), "org-b", fakePrincipalAdapter{active: true}
		},
		"inactive user": func(t *testing.T) (context.Context, string, fakePrincipalAdapter) {
			t.Helper()
			return governedContext(t, "org", "user"), "org", fakePrincipalAdapter{active: false}
		},
		"session-backed JWT cross tenant": func(t *testing.T) (context.Context, string, fakePrincipalAdapter) {
			t.Helper()
			return governedChatContext(t, "org-a", "user"), "org-b", fakePrincipalAdapter{active: true}
		},
		"session-backed JWT inactive user": func(t *testing.T) (context.Context, string, fakePrincipalAdapter) {
			t.Helper()
			return governedChatContext(t, "org", "user"), "org", fakePrincipalAdapter{active: false}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, org, principal := build(t)
			checkpoint, evaluation := testCheckpoint(t, principal, noMatch)
			err := checkpoint.Check(ctx, org)
			var unavailable *InfrastructureUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Zero(t, evaluation.calls)
		})
	}

	for name, classify := range map[string]func(context.Context) (context.Context, error){
		"internal": func(ctx context.Context) (context.Context, error) {
			return WithInternal(ctx, CallCategoryPromptScanner)
		},
		"background": func(ctx context.Context) (context.Context, error) {
			return WithBackground(ctx, CallCategoryAutomaticChatTitle)
		},
		"unsupported": func(ctx context.Context) (context.Context, error) {
			return WithUnsupported(ctx, CallCategoryAssistantChat)
		},
	} {
		t.Run(name+" bypass", func(t *testing.T) {
			t.Parallel()
			ctx, err := classify(t.Context())
			require.NoError(t, err)
			checkpoint, evaluation := testCheckpoint(t, fakePrincipalAdapter{}, noMatch)
			require.NoError(t, checkpoint.Check(ctx, "org"))
			require.Zero(t, evaluation.calls)
		})
	}
}

func TestSessionBackedChatJWTClassificationIsGoverned(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.WithValidatedChatSessionActingUser(t.Context(), "org", "user", "session")
	classified, err := WithGovernedUser(ctx, CallCategoryUserChatCompletion)
	require.NoError(t, err)
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	checkpoint, evaluation := testCheckpoint(t, fakePrincipalAdapter{active: true}, noMatch)
	require.NoError(t, checkpoint.Check(classified, "org"))
	require.Equal(t, 1, evaluation.calls)
}

func TestClassificationRejectsUnknownAndSubstituteIdentity(t *testing.T) {
	t.Parallel()
	_, err := WithInternal(t.Context(), CallCategory("new-unregistered-path"))
	require.Error(t, err)
	_, err = WithInternal(t.Context(), CallCategoryUserChatCompletion)
	require.Error(t, err)

	chatSessionID := "chat-session"
	for name, buildContext := range map[string]func(*testing.T) context.Context{
		"api key creator": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "api-key-owner", APIKeyID: "key"})
		},
		"assistant owner": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "assistant-owner"})
		},
		"chat session": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "chat-user", SessionID: &chatSessionID})
		},
		"external identity": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", ExternalUserID: "external"})
		},
		"anonymous organization": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org"})
		},
		"shared demo session": func(t *testing.T) context.Context {
			t.Helper()
			return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: constants.DemoOrganizationID, UserID: "user", SessionID: &chatSessionID}, false)
		},
		"absent identity": func(t *testing.T) context.Context {
			t.Helper()
			return t.Context()
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := WithGovernedUser(buildContext(t), CallCategoryUserChatCompletion)
			require.Error(t, err)
		})
	}
}
