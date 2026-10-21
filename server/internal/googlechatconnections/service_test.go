package googlechatconnections

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/google_chat_connections"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/provider"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestConnectionSecretsNeverLeaveList(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	require.NoError(t, f.service.saveAuthorization(ctx, f.auth, &oauth2.Token{AccessToken: "synthetic-secret", RefreshToken: "synthetic-refresh"}, provider.Identity{Subject: "123", Email: "person@example.test", Verified: true}))
	list, err := f.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, list.Connections, 1)
	rows, err := repo.New(f.db).ListConnections(ctx, f.auth.ActiveOrganizationID)
	require.NoError(t, err)
	require.NotContains(t, rows[0].CredentialsEncrypted.String, "synthetic-secret")
	raw, err := f.enc.Decrypt(rows[0].CredentialsEncrypted.String)
	require.NoError(t, err)
	require.Contains(t, raw, "synthetic-secret")
	require.NoError(t, f.service.Disconnect(ctx, &gen.DisconnectPayload{ID: rows[0].ID.String(), Generation: rows[0].Generation.String()}))
	row, err := repo.New(f.db).GetConnection(ctx, repo.GetConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: rows[0].ID})
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	require.Equal(t, "disconnected", row.Status)
}
func TestMappingIsVersionedAndTenantBound(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	q := repo.New(f.db)
	connection, err := q.SaveConnection(ctx, repo.SaveConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, GoogleSubject: "123", Email: "person@example.test", CredentialsEncrypted: conv.ToPGText("encrypted"), GrantedScopes: []string{}})
	require.NoError(t, err)
	assistant, err := assistantrepo.New(f.db).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: *f.auth.ProjectID, OrganizationID: f.auth.ActiveOrganizationID, CreatedByUserID: conv.ToPGText(f.auth.UserID), Name: "Test assistant", Model: "test", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	b, err := q.CreateBinding(ctx, repo.CreateBindingParams{ID: uuid.New(), OrganizationID: f.auth.ActiveOrganizationID, ProjectID: *f.auth.ProjectID, ConnectionID: connection.ID, AssistantID: assistant.ID, SpaceName: "spaces/test", DisplayName: "Test", Prefix: "assistant:"})
	require.NoError(t, err)
	require.NoError(t, q.ObserveMember(ctx, repo.ObserveMemberParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, GoogleUserName: "users/external", DisplayName: "External"}))
	members, err := q.ListMembers(ctx, repo.ListMembersParams{OrganizationID: b.OrganizationID, BindingID: b.ID})
	require.NoError(t, err)
	require.Len(t, members, 1)
	mapped, err := f.service.MapMember(ctx, &gen.MapMemberPayload{BindingID: b.ID.String(), MemberID: members[0].ID.String(), UserID: &f.auth.UserID, Revision: 0})
	if err != nil {
		t.Logf("mapping cause: %v", errors.Unwrap(err))
	}
	require.NoError(t, err)
	require.Equal(t, f.auth.UserID, *mapped.UserID)
	_, err = f.service.MapMember(ctx, &gen.MapMemberPayload{BindingID: b.ID.String(), MemberID: members[0].ID.String(), Revision: 0})
	require.Error(t, err)
	_, err = q.GetBinding(ctx, repo.GetBindingParams{OrganizationID: "another-tenant", ID: b.ID})
	require.Error(t, err)
	b.Status = "active"
	_, err = q.UpdateBinding(ctx, repo.UpdateBindingParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: b.ID, Revision: b.Revision, Status: "active"})
	require.NoError(t, err)
	lookup := repo.ResolveMappedUserParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, AssistantID: assistant.ID, GoogleUserName: "users/external"}
	user, err := q.ResolveMappedUser(ctx, lookup)
	require.NoError(t, err)
	require.Equal(t, f.auth.UserID, user)
	lookup.GoogleUserName = "users/unmapped"
	_, err = q.ResolveMappedUser(ctx, lookup)
	require.Error(t, err)
	require.NoError(t, q.MarkMembersAbsent(ctx, repo.MarkMembersAbsentParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, PresentUsers: []string{}}))
	lookup.GoogleUserName = "users/external"
	_, err = q.ResolveMappedUser(ctx, lookup)
	require.Error(t, err)
	require.NotNil(t, f.auth.Email)
	require.NoError(t, q.ObserveMember(ctx, repo.ObserveMemberParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID, GoogleUserName: "users/auto", DisplayName: "Matching member", Email: *f.auth.Email}))
	auto := repo.AutoMapMembersParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, BindingID: b.ID}
	matches, err := q.AutoMapMembers(ctx, auto)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, f.auth.UserID, matches[0].UserID.String)
	_, err = q.MapMember(ctx, repo.MapMemberParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: matches[0].ID, Revision: matches[0].Revision})
	require.NoError(t, err)
	matches, err = q.AutoMapMembers(ctx, auto)
	require.NoError(t, err)
	require.Empty(t, matches, "sync must preserve an administrator's explicit unmapping")
}

func TestPostedTriageThreadResumesOriginatingChat(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	q := repo.New(f.db)
	connection, err := q.SaveConnection(ctx, repo.SaveConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, GoogleSubject: "123", Email: "person@example.test", CredentialsEncrypted: conv.ToPGText("encrypted"), GrantedScopes: []string{}})
	require.NoError(t, err)
	assistant, err := assistantrepo.New(f.db).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: *f.auth.ProjectID, OrganizationID: f.auth.ActiveOrganizationID, CreatedByUserID: conv.ToPGText(f.auth.UserID), Name: "Triage", Model: "test", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	b, err := q.CreateBinding(ctx, repo.CreateBindingParams{ID: uuid.New(), OrganizationID: f.auth.ActiveOrganizationID, ProjectID: *f.auth.ProjectID, ConnectionID: connection.ID, AssistantID: assistant.ID, SpaceName: "spaces/test", DisplayName: "Test", Prefix: "assistant:"})
	require.NoError(t, err)
	trigger, err := triggerrepo.New(f.db).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, DefinitionSlug: "googlechat", Name: "Chat", TargetDisplay: "Triage", TargetKind: "assistant", TargetRef: assistant.ID.String(), ConfigJson: []byte(`{}`), Status: "active"})
	require.NoError(t, err)
	b, err = q.UpdateBinding(ctx, repo.UpdateBindingParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: b.ID, Revision: b.Revision, Status: "active", TriggerID: uuid.NullUUID{UUID: trigger.ID, Valid: true}})
	require.NoError(t, err)
	chat := uuid.New()
	require.NoError(t, assistantrepo.New(f.db).UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chat, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID}))
	_, err = assistantrepo.New(f.db).UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: assistant.ID, ProjectID: b.ProjectID, CorrelationID: "github:issue:123", ChatID: chat, SourceKind: "github", SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	runtime := &provider.Runtime{DB: f.db}
	env := toolconfig.ToolCallEnv{GramChatID: chat.String()}
	body := []byte(`{"name":"spaces/test/messages/summary","thread":{"name":"spaces/test/threads/issue"}}`)
	require.NoError(t, runtime.RecordReply(ctx, env, body))
	require.NoError(t, runtime.RecordReply(ctx, env, body))
	route, err := triggerrepo.New(f.db).GetTriggerThreadRoute(ctx, triggerrepo.GetTriggerThreadRouteParams{ProjectID: b.ProjectID, TargetKind: "assistant", TargetRef: assistant.ID.String(), CorrelationID: "googlechat:" + trigger.ID.String() + ":spaces/test/threads/issue"})
	require.NoError(t, err)
	require.Equal(t, "github:issue:123", route.RouteToCorrelationID.String)
	require.Equal(t, "subscribed", route.State)
	other := uuid.New()
	require.NoError(t, assistantrepo.New(f.db).UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: other, ProjectID: b.ProjectID, OrganizationID: b.OrganizationID}))
	_, err = assistantrepo.New(f.db).UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: assistant.ID, ProjectID: b.ProjectID, CorrelationID: "github:issue:another", ChatID: other, SourceKind: "github", SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, runtime.RecordReply(ctx, toolconfig.ToolCallEnv{GramChatID: other.String()}, body))
	unchanged, err := triggerrepo.New(f.db).GetTriggerThreadRoute(ctx, triggerrepo.GetTriggerThreadRouteParams{ProjectID: b.ProjectID, TargetKind: "assistant", TargetRef: assistant.ID.String(), CorrelationID: route.CorrelationID})
	require.NoError(t, err)
	require.Equal(t, "github:issue:123", unchanged.RouteToCorrelationID.String, "posting again must not take over an existing conversation")
	_, err = f.service.UpdateBinding(ctx, &gen.UpdateBindingPayload{BindingID: b.ID.String(), Revision: b.Revision, Action: "unsupported"})
	require.Error(t, err)
	untouched, err := triggerrepo.New(f.db).GetTriggerInstanceByIDPublic(ctx, trigger.ID)
	require.NoError(t, err)
	require.Equal(t, "active", untouched.Status, "invalid actions must not pause a trigger before returning an error")
}
