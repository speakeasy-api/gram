package adminmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// stubWriter stands in for an operation that has an implementation.
type stubWriter struct{}

func (stubWriter) revalidate(context.Context, pgx.Tx, Proposal) error { return nil }
func (stubWriter) view(Proposal) (proposalView, error)                { return proposalView{}, nil }
func (stubWriter) execution(writeAuthority) ProposalExecution         { return ProposalExecution{} }
func (stubWriter) registerPrepare(*mcp.Server)                        {}

func writeContext(t *testing.T, f proposalFixture) context.Context {
	t.Helper()
	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	principal := Principal{Subject: f.owner.SubjectURN, Email: staff.Email, ClientID: "test-client", ClientRowID: f.owner.ClientRowID.String(), ConnectionID: f.owner.ConnectionID.String(), Generation: f.owner.Generation.String(), Scopes: []string{ScopeRead, ScopeWrite}, staff: staff}
	return contextvalues.SetAdminAuthContext(context.WithValue(t.Context(), principalKey{}, principal), staff)
}

func TestWriteToolsDispatchByStoredOperation(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_write_dispatch")
	ctx := writeContext(t, f)

	feature, _, err := f.store.Create(t.Context(), f.owner, featureProposal(f.orgA, "dispatch-feature", true), time.Now())
	require.NoError(t, err)
	onboardingInput := featureProposal(f.orgA, "dispatch-onboarding", true)
	onboardingInput.Operation = OperationSetOrganizationOnboarding
	onboarding, _, err := f.store.Create(t.Context(), f.owner, onboardingInput, time.Now())
	require.NoError(t, err)

	both := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true, OperationSetOrganizationOnboarding: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	featureOnly := newWriteTools(f.store, both, "", map[WriteOperation]operationWriter{OperationSetOrganizationFeature: stubWriter{}})                       //nolint:exhaustive // Only selected operations are implemented by this test.
	out, err := featureOnly.status(ctx, ProposalIDInput{ProposalID: feature.ID.String()})
	require.NoError(t, err)
	require.Equal(t, string(OperationSetOrganizationFeature), out.Operation)
	_, err = featureOnly.status(ctx, ProposalIDInput{ProposalID: onboarding.ID.String()})
	require.ErrorIs(t, err, ErrProposalNotFound, "a stored operation without a writer is not reachable")
	_, err = featureOnly.execute(ctx, ProposalIDInput{ProposalID: onboarding.ID.String()})
	require.ErrorIs(t, err, ErrProposalNotFound)

	// The switch checked is the one for the stored operation, not any enabled one.
	onboardingOnly := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationOnboarding: true}}                                                              //nolint:exhaustive // Only selected write operations are enabled by this test.
	tools := newWriteTools(f.store, onboardingOnly, "", map[WriteOperation]operationWriter{OperationSetOrganizationFeature: stubWriter{}, OperationSetOrganizationOnboarding: stubWriter{}}) //nolint:exhaustive // Only selected operations are implemented by this test.
	_, err = tools.status(ctx, ProposalIDInput{ProposalID: feature.ID.String()})
	require.ErrorIs(t, err, ErrWriteDisabled)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: feature.ID.String()})
	require.ErrorIs(t, err, ErrWriteDisabled)
	_, err = tools.status(ctx, ProposalIDInput{ProposalID: onboarding.ID.String()})
	require.NoError(t, err)
}

func TestAttachWritesRejectsUnimplementedOperation(t *testing.T) {
	t.Parallel()
	oauth := &StaffOAuth{Approval: &StaffProposalApproval{}}
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationExtendOrganizationTrial: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	err := AttachWrites(&Runtime{}, oauth, &productfeatures.Client{}, writes)
	require.ErrorContains(t, err, "not implemented")
	require.Nil(t, oauth.Approval.operations, "nothing becomes approvable after a configuration error")

	require.NoError(t, AttachWrites(&Runtime{}, oauth, &productfeatures.Client{}, WriteConfig{}))
	require.Contains(t, oauth.Approval.operations, OperationSetOrganizationFeature)
	require.Contains(t, oauth.Approval.operations, OperationSetOrganizationOnboarding)
}

func TestFeatureViewRequiresMatchingTarget(t *testing.T) {
	t.Parallel()
	writer := &featureWriter{}
	preview, err := json.Marshal(featurePreview{OrganizationID: "org_b", Name: "B", Slug: "b", Feature: "logs", Before: false, After: true})
	require.NoError(t, err)

	_, err = writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_a"}, Preview: preview})
	require.ErrorIs(t, err, ErrProposalInvalidated, "a preview naming another organisation is never shown")

	view, err := writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_b"}, Preview: preview})
	require.NoError(t, err)
	require.Equal(t, "Turn the logs feature on", view.Summary)
	require.Equal(t, []proposalViewChange{{Setting: "logs feature", Before: "Off", After: "On"}}, view.Changes)
}

func TestApprovalPageEscapesStoredValues(t *testing.T) {
	t.Parallel()
	var page strings.Builder
	err := proposalApprovalPage.Execute(&page, proposalApprovalView{
		ID:        "proposal",
		Operation: string(OperationSetOrganizationFeature),
		Change:    proposalView{Summary: "Turn the logs feature on", OrganizationID: "org_a", OrganizationName: `<script>alert(1)</script>`, OrganizationSlug: `"><img src=x>`},
		Preview:   `{"name":"<script>alert(1)</script>"}`,
	})
	require.NoError(t, err)
	require.NotContains(t, page.String(), "<script>")
	require.NotContains(t, page.String(), "<img")
	require.Contains(t, page.String(), "&lt;script&gt;alert(1)&lt;/script&gt;")
}
