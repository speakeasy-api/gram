package email

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

type mockSender struct {
	mock.Mock
}

func (m *mockSender) SendTransactional(ctx context.Context, input loops.SendTransactionalInput) error {
	args := m.Called(ctx, input)
	return args.Error(0)
}

func newMockSender(t *testing.T) *mockSender {
	t.Helper()
	m := &mockSender{}
	t.Cleanup(func() { m.AssertExpectations(t) })
	return m
}

func TestService_Send_TranslatesTemplateToLoopsInput(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	tmpl := TeamInvite{
		InviteLink:       "https://app.gram.sh/invite?token=xyz",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	}

	expected := loops.SendTransactionalInput{
		TransactionalID: string(transactionalIDTeamInvite),
		Email:           "carol@example.com",
		DataVariables: map[string]string{
			"invite_link":       "https://app.gram.sh/invite?token=xyz",
			"inviter_name":      "Bob",
			"inviter_email":     "bob@example.com",
			"organization_name": "Acme Corp",
		},
		AddToAudience: true,
	}

	sender.On("SendTransactional", mock.Anything, expected).Return(nil).Once()

	err := svc.Send(t.Context(), "carol@example.com", tmpl)
	require.NoError(t, err)
}

func TestService_Send_EmptyRecipientReturnsSentinel(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	err := svc.Send(t.Context(), "", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	})
	require.ErrorIs(t, err, ErrEmptyRecipient)
}

func TestService_Send_UnregisteredTemplateReturnsSentinel(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	err := svc.Send(t.Context(), "user@example.com", unregisteredTemplate{})
	require.ErrorIs(t, err, ErrUnregisteredTemplate)
}

func TestService_Send_PropagatesSenderError(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	transportErr := errors.New("transport boom")
	sender.On("SendTransactional", mock.Anything, mock.Anything).Return(transportErr).Once()

	err := svc.Send(t.Context(), "user@example.com", TeamInvite{
		InviteLink:       "https://example.com",
		InviterName:      "Bob",
		InviterEmail:     "bob@example.com",
		OrganizationName: "Acme Corp",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, transportErr)
	require.Contains(t, err.Error(), string(transactionalIDTeamInvite))
}

func TestService_Send_RespectsTemplateAddToAudienceFlag(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	sender.On("SendTransactional", mock.Anything, mock.MatchedBy(func(in loops.SendTransactionalInput) bool {
		return !in.AddToAudience && in.TransactionalID == "no-audience-id"
	})).Return(nil).Once()

	err := svc.Send(t.Context(), "user@example.com", noAudienceTemplate{})
	require.NoError(t, err)
}

func TestService_Send_NilVariablesAreForwarded(t *testing.T) {
	t.Parallel()

	sender := newMockSender(t)
	svc := NewService(testenv.NewLogger(t), sender)

	sender.On("SendTransactional", mock.Anything, mock.MatchedBy(func(in loops.SendTransactionalInput) bool {
		return in.DataVariables == nil
	})).Return(nil).Once()

	err := svc.Send(t.Context(), "user@example.com", nilVarsTemplate{})
	require.NoError(t, err)
}

// Test-only template implementations.

type unregisteredTemplate struct{}

func (unregisteredTemplate) TransactionalID() TransactionalID { return "" }
func (unregisteredTemplate) Variables() map[string]string     { return nil }
func (unregisteredTemplate) AddToAudience() bool              { return false }

type noAudienceTemplate struct{}

func (noAudienceTemplate) TransactionalID() TransactionalID {
	return TransactionalID("no-audience-id")
}
func (noAudienceTemplate) Variables() map[string]string { return map[string]string{"a": "b"} }
func (noAudienceTemplate) AddToAudience() bool          { return false }

type nilVarsTemplate struct{}

func (nilVarsTemplate) TransactionalID() TransactionalID { return TransactionalID("nil-vars-id") }
func (nilVarsTemplate) Variables() map[string]string     { return nil }
func (nilVarsTemplate) AddToAudience() bool              { return false }
