package promptpolicy_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func capturingPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			f, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, f)
		})
	return pub, &published
}

func newRequest(content string) *riskv1.PromptPolicyAnalysis {
	modelConfig := []byte(`{"model":"openai/gpt-4.1-mini","fail_open":false}`)
	return riskv1.PromptPolicyAnalysis_builder{
		RequestId:         new("req-1"),
		ChatMessageId:     new("msg-1"),
		ProjectId:         new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:    new("org-1"),
		RiskPolicyId:      new("policy-1"),
		RiskPolicyVersion: new(int64(3)),
		CreatedAt:         new("2026-06-20T00:00:00Z"),
		Content:           &content,
		UserId:            new("user-1"),
		Prompt:            new("block unsafe requests"),
		ModelConfig:       modelConfig,
		MessageType:       new("user_message"),
		Body:              &content,
		ToolName:          new(""),
	}.Build()
}

func TestHandle_PublishesPromptPolicyFinding(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		require.Equal(t, "org-1", in.OrgID)
		require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a313", in.ProjectID)
		require.Equal(t, "user-1", in.UserID)
		require.Equal(t, "block unsafe requests", in.Prompt)
		require.Equal(t, "delete production", in.Message.Body)
		require.Equal(t, "openai/gpt-4.1-mini", in.Config.Model)
		require.False(t, in.Config.FailOpen)
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.88,
			Rationale:        "Message matched the prompt-based policy.",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	}
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	stubScanner := promptpolicy.NewScanner(testenv.NewLogger(t), promptpolicy.NoopEvaluator)
	flags := &recordingFlagProvider{enabled: true}
	gate := scanners.NewAsyncShadowGate(testenv.NewLogger(t), flags, fakeFlagGroupDB{})
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, stubScanner, pub, gate)

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))

	require.Len(t, flags.calls, 1)
	require.Equal(t, feature.FlagRiskAsyncScanShadow, flags.calls[0].flag)
	require.Equal(t, "msg-1", flags.calls[0].distinctID)
	require.Nil(t, flags.calls[0].groups)
	require.Equal(t, map[string]string{"organization_slug": "org-slug", "project_slug": "project-slug"}, flags.calls[0].personProperties)
	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, promptpolicy.Source, f.GetSource())
	require.Equal(t, promptpolicy.Rule, f.GetRuleId())
	require.Equal(t, "req-1", f.GetRequestId())
	require.Equal(t, "msg-1", f.GetChatMessageId())
	require.Equal(t, int64(3), f.GetRiskPolicyVersion())
	require.NotEmpty(t, f.GetId())
	require.InDelta(t, 0.88, f.GetConfidence(), 0.0001)
}

func TestHandle_CleanPromptPolicyContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{
			Matched:          false,
			Confidence:       0,
			Rationale:        "",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	}
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	stubScanner := promptpolicy.NewScanner(testenv.NewLogger(t), promptpolicy.NoopEvaluator)
	flags := &recordingFlagProvider{enabled: true}
	gate := scanners.NewAsyncShadowGate(testenv.NewLogger(t), flags, fakeFlagGroupDB{})
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, stubScanner, pub, gate)

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

func TestHandle_FlagOffUsesStubPromptPolicyScanner(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		t.Fatal("real scanner must not be invoked when async shadow flag is off")
		return nil, nil
	})
	stubScanner := promptpolicy.NewScanner(testenv.NewLogger(t), promptpolicy.NoopEvaluator)
	flags := &recordingFlagProvider{enabled: false}
	gate := scanners.NewAsyncShadowGate(testenv.NewLogger(t), flags, fakeFlagGroupDB{})
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, stubScanner, pub, gate)

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))
	require.Len(t, flags.calls, 1)
	require.Empty(t, *published)
}

func TestHandle_ProjectSlugLookupErrorUsesStubPromptPolicyScanner(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		t.Fatal("real scanner must not be invoked when async shadow flag slug lookup fails")
		return nil, nil
	})
	stubScanner := promptpolicy.NewScanner(testenv.NewLogger(t), promptpolicy.NoopEvaluator)
	flags := &recordingFlagProvider{enabled: true}
	gate := scanners.NewAsyncShadowGate(testenv.NewLogger(t), flags, fakeFlagGroupDB{err: errors.New("lookup failed")})
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, stubScanner, pub, gate)

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))
	require.Empty(t, flags.calls)
	require.Empty(t, *published)
}

func TestHandle_FlagErrorUsesStubPromptPolicyScanner(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		t.Fatal("real scanner must not be invoked when async shadow flag evaluation fails")
		return nil, nil
	})
	stubScanner := promptpolicy.NewScanner(testenv.NewLogger(t), promptpolicy.NoopEvaluator)
	flags := &recordingFlagProvider{enabled: true, err: errors.New("flag failed")}
	gate := scanners.NewAsyncShadowGate(testenv.NewLogger(t), flags, fakeFlagGroupDB{})
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, stubScanner, pub, gate)

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))
	require.Len(t, flags.calls, 1)
	require.Empty(t, *published)
}

type flagCall struct {
	flag             feature.Flag
	distinctID       string
	groups           map[string]string
	personProperties map[string]string
}

type recordingFlagProvider struct {
	enabled bool
	err     error
	calls   []flagCall
}

func (p *recordingFlagProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (p *recordingFlagProvider) IsFlagEnabledLocal(_ context.Context, flag feature.Flag, distinctID string, groups, personProperties map[string]string) (bool, error) {
	p.calls = append(p.calls, flagCall{
		flag:             flag,
		distinctID:       distinctID,
		groups:           groups,
		personProperties: cloneStringMap(personProperties),
	})
	return p.enabled, p.err
}

func (p *recordingFlagProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

type fakeFlagGroupDB struct {
	err error
}

func (d fakeFlagGroupDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (d fakeFlagGroupDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (d fakeFlagGroupDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeFlagGroupRow(d)
}

func (d fakeFlagGroupDB) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}

type fakeFlagGroupRow struct {
	err error
}

func (r fakeFlagGroupRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	orgSlug, ok := dest[0].(*string)
	if !ok {
		return errors.New("organization slug destination must be *string")
	}
	projectSlug, ok := dest[1].(*string)
	if !ok {
		return errors.New("project slug destination must be *string")
	}
	*orgSlug = "org-slug"
	*projectSlug = "project-slug"
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
