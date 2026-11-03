package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateSessionHandoffLinkConfirmsBeforeFeatureOrContentWork(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_handoff_confirmation")
	require.NoError(t, err)
	enabled := true
	svc, _ := newRecallService(t, conn, &enabled)
	featureReads := 0
	svc.portability = func(context.Context, string) (bool, error) { featureReads++; return true, nil }
	svc.WithHandoffMinter(&recordingHandoffMinter{})
	_, err = svc.CreateSessionHandoffLink(ctx, registrationServicePrincipal(), CreateSessionHandoffLinkInput{SessionID: "ses_missing", Confirmed: false})
	require.ErrorIs(t, err, errSessionHandoffConfirmationRequired)
	require.Zero(t, featureReads, "confirmation must precede feature, budget, ownership, and transcript reads")
}

func TestCreateSessionHandoffLinkUsesCanonicalChatIDAndDefaultTTL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_handoff_identity")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc, _ := newRecallService(t, conn, &enabled)
	minter := &recordingHandoffMinter{}
	svc.WithHandoffMinter(minter)
	sessionID := "external-session-" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "handoff title", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{role: "user", content: "continue the safe work", at: time.Now().UTC(), generation: 1, userID: principal.UserID, source: "cursor", analyzed: true})

	continued, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	_, err = svc.CreateSessionHandoffLink(ctx, principal, CreateSessionHandoffLinkInput{SessionID: sessionID, Confirmed: true})
	require.NoError(t, err)
	require.Len(t, minter.inputs, 1)
	minted := minter.inputs[0]
	require.Equal(t, continued.Digest, minted.Content)
	require.Equal(t, chatID, minted.ChatID)
	require.Equal(t, sessionID, minted.SessionID)
	require.NotEqual(t, sessionID, minted.ChatID.String())
	require.Equal(t, 900, *minted.TTLSeconds)
}

func TestCreateSessionHandoffLinkClampsTTLAndHidesForeignSessions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_handoff_ttl_ownership")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc, _ := newRecallService(t, conn, &enabled)
	minter := &recordingHandoffMinter{}
	svc.WithHandoffMinter(minter)
	owned := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, owned, "owned", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{role: "user", content: "safe", at: time.Now().UTC(), generation: 1, userID: principal.UserID, source: "cursor", analyzed: true})
	foreign := "ses_" + uuid.NewString()
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, "other-user", foreign, "foreign", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	low, high := 1, 999999
	for _, input := range []CreateSessionHandoffLinkInput{{SessionID: owned, Confirmed: true}, {SessionID: owned, Confirmed: true, TTLSeconds: &low}, {SessionID: owned, Confirmed: true, TTLSeconds: &high}} {
		_, err := svc.CreateSessionHandoffLink(ctx, principal, input)
		require.NoError(t, err)
	}
	require.Equal(t, []int{900, 60, 3600}, []int{*minter.inputs[0].TTLSeconds, *minter.inputs[1].TTLSeconds, *minter.inputs[2].TTLSeconds})
	_, foreignErr := svc.CreateSessionHandoffLink(ctx, principal, CreateSessionHandoffLinkInput{SessionID: foreign, Confirmed: true})
	_, unknownErr := svc.CreateSessionHandoffLink(ctx, principal, CreateSessionHandoffLinkInput{SessionID: "ses_" + uuid.NewString(), Confirmed: true})
	require.ErrorIs(t, foreignErr, errSessionNotFound)
	require.ErrorIs(t, unknownErr, errSessionNotFound)
	require.Equal(t, foreignErr.Error(), unknownErr.Error())
}
