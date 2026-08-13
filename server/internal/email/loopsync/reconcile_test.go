package loopsync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeAPI struct {
	listed        []TransactionalEmail
	transactional TransactionalEmail
	message       EmailMessage
	guardian      GuardianResult
	created       int
	updated       int
	published     int
}

func (f *fakeAPI) ListTransactionalEmails(context.Context) ([]TransactionalEmail, error) {
	return f.listed, nil
}

func (f *fakeAPI) GetTransactionalEmail(_ context.Context, _ string) (TransactionalEmail, error) {
	return f.transactional, nil
}

func (f *fakeAPI) CreateTransactionalEmail(_ context.Context, name string) (TransactionalEmail, error) {
	f.created++
	f.transactional.Name = name
	return f.transactional, nil
}

func (f *fakeAPI) EnsureDraft(context.Context, string) (TransactionalEmail, error) {
	return f.transactional, nil
}

func (f *fakeAPI) GetEmailMessage(context.Context, string) (EmailMessage, error) {
	return f.message, nil
}

func (f *fakeAPI) UpdateEmailMessage(_ context.Context, _ string, input UpdateEmailMessageInput) (EmailMessage, error) {
	f.updated++
	f.message.Subject = input.Subject
	f.message.PreviewText = input.PreviewText
	f.message.FromName = input.FromName
	f.message.FromEmail = input.FromEmail
	f.message.ReplyToEmail = input.ReplyToEmail
	f.message.LMX = input.LMX
	return f.message, nil
}

func (f *fakeAPI) Guardian(context.Context, string) (GuardianResult, error) {
	return f.guardian, nil
}

func (f *fakeAPI) Publish(context.Context, string) error {
	f.published++
	return nil
}

func testManifest() *Manifest {
	return &Manifest{
		Version: 1,
		Defaults: MessageDefaults{
			FromName:     "Speakeasy",
			FromEmail:    "gram",
			ReplyToEmail: "gram@speakeasy.com",
		},
		Templates: map[string]TemplateSpec{
			"team_invite": {
				ManagedName:     "gram.transactional.v2.team_invite",
				Subject:         "Join {data.organization_name}",
				PreviewText:     "Invitation",
				Source:          "team_invite.lmx",
				Variables:       []string{"organization_name"},
				UnusedVariables: nil,
				LMX:             "<Paragraph>Hello {data.organization_name}</Paragraph>",
			},
		},
		Dir: "",
	}
}

func TestReconcile_CreatesPublishesAndReturnsResolvedID(t *testing.T) {
	t.Parallel()

	draftID := "message-1"
	api := &fakeAPI{
		transactional: TransactionalEmail{ID: "transactional-1", DraftEmailMessageID: &draftID},
		message:       EmailMessage{ID: draftID, ContentRevisionID: "revision-1"},
	}

	resolved, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"team_invite": "transactional-1"}, resolved)
	require.Equal(t, 1, api.created)
	require.Equal(t, 1, api.updated)
	require.Equal(t, 1, api.published)
}

func TestReconcile_AlwaysUpdatesAndPublishesExistingTemplate(t *testing.T) {
	t.Parallel()

	draftID := "message-draft"
	transactional := TransactionalEmail{
		ID:                  "transactional-existing",
		Name:                "gram.transactional.v2.team_invite",
		DraftEmailMessageID: &draftID,
	}
	api := &fakeAPI{
		listed:        []TransactionalEmail{transactional},
		transactional: transactional,
		message: EmailMessage{
			ID:                draftID,
			Subject:           "Join {data.organization_name}",
			PreviewText:       "Invitation",
			FromName:          "Speakeasy",
			FromEmail:         "gram",
			ReplyToEmail:      "gram@speakeasy.com",
			LMX:               "<Paragraph>Hello {data.organization_name}</Paragraph>",
			ContentRevisionID: "revision-1",
		},
	}

	resolved, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "transactional-existing", resolved["team_invite"])
	require.Zero(t, api.created)
	require.Equal(t, 1, api.updated)
	require.Equal(t, 1, api.published)
}

func TestReconcile_RecoversUncommittedCreationByManagedName(t *testing.T) {
	t.Parallel()

	draftID := "message-draft"
	transactional := TransactionalEmail{
		ID:                  "transactional-existing",
		Name:                "gram.transactional.v2.team_invite",
		DraftEmailMessageID: &draftID,
	}
	api := &fakeAPI{
		listed:        []TransactionalEmail{transactional},
		transactional: transactional,
		message:       EmailMessage{ID: draftID, ContentRevisionID: "revision-1"},
	}

	resolved, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{})
	require.NoError(t, err)
	require.Equal(t, "transactional-existing", resolved["team_invite"])
	require.Zero(t, api.created)
	require.Equal(t, 1, api.updated)
	require.Equal(t, 1, api.published)
}

func TestReconcile_TreatsSuccessfulPublishAsSuccess(t *testing.T) {
	t.Parallel()

	draftID := "message-1"
	api := &fakeAPI{
		transactional: TransactionalEmail{ID: "transactional-1", DraftEmailMessageID: &draftID},
		message:       EmailMessage{ID: draftID, ContentRevisionID: "revision-1"},
	}

	_, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{})
	require.NoError(t, err)
	require.Equal(t, 1, api.published)
}

func TestReconcile_RejectsMappedIDForDifferentManagedName(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{transactional: TransactionalEmail{ID: "wrong", Name: "legacy invite"}}
	_, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{"team_invite": "wrong"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "belongs to")
}

func TestReconcile_GuardianErrorsBlockPublish(t *testing.T) {
	t.Parallel()

	draftID := "message-1"
	api := &fakeAPI{
		transactional: TransactionalEmail{ID: "transactional-1", DraftEmailMessageID: &draftID},
		message:       EmailMessage{ID: draftID, ContentRevisionID: "revision-1"},
		guardian:      GuardianResult{Errors: []GuardianIssue{{Rule: "missingButtonHrefs", Description: "button needs a link"}}},
	}

	_, err := (&Reconciler{API: api}).Reconcile(t.Context(), testManifest(), map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Guardian rejected")
	require.Zero(t, api.published)
}
