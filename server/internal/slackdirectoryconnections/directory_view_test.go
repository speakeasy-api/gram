package slackdirectoryconnections_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
)

func directory(entries ...slackdirectoryconnections.DirectoryMember) directoryFunc {
	return func(_ context.Context, _, _ string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		report(slackdirectoryconnections.SyncProgress{Phase: "fetching", Pages: 1, Members: len(entries), ExcludedExternal: 0, Bots: 0})
		return entries, nil
	}
}

func entry(id, name, email, status, memberType string) slackdirectoryconnections.DirectoryMember {
	return slackdirectoryconnections.DirectoryMember{UserID: id, DisplayName: name, Email: email, Status: status, MemberType: memberType, UpdatedAt: nil}
}

func names(page *gen.ListMembersResult) []string {
	out := make([]string, 0, len(page.Members))
	for _, m := range page.Members {
		out = append(out, conv.PtrValOr(m.DisplayName, ""))
	}
	return out
}

func TestDirectoryHidesDeactivatedAndBotsAndSortsUnmappedFirst(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	person := addPerson(t, ctx, f)
	require.NoError(t, syncer(f, directory(
		entry("UEXAMPLE01", "Aaron Mapped", person+"@demo.getgram.ai", "active", "person"),
		entry("UEXAMPLE02", "charlie", "charlie@example.com", "active", "person"),
		entry("UEXAMPLE03", "Bea", "bea@example.com", "active", "person"),
		entry("UEXAMPLE04", "Dee Former", "dee@example.com", "deactivated", "person"),
		entry("UEXAMPLE05", "Figma", "", "active", "bot"),
		entry("UEXAMPLE06", "Gail Guest", "gail@example.com", "active", "single_channel_guest"),
	)).Run(ctx, syncRequest(f, c), nil))

	page, err := f.service.ListMembers(ctx, memberRequest())
	require.NoError(t, err)
	require.Equal(t, int64(3), page.Total)
	require.Equal(t, []string{"Bea", "charlie", "Aaron Mapped"}, names(page))

	p := memberRequest()
	p.IncludeDeactivated = true
	p.IncludeBots = true
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(5), page.Total)
	require.Equal(t, []string{"Bea", "charlie", "Dee Former", "Figma", "Aaron Mapped"}, names(page))

	p.IncludeGuests = true
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(6), page.Total)
	require.Equal(t, []string{"Bea", "charlie", "Dee Former", "Figma", "Gail Guest", "Aaron Mapped"}, names(page))

	p.Limit = 2
	p.Page = 3
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(6), page.Total)
	require.Equal(t, []string{"Gail Guest", "Aaron Mapped"}, names(page))
}

func TestSyncMapsUniqueActiveEmailMatchesOnce(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	matched, guest, inactive := addPerson(t, ctx, f), addPerson(t, ctx, f), addPerson(t, ctx, f)
	snapshot := directory(
		entry("UEXAMPLE01", "Matched", " "+matched+"@DEMO.getgram.ai ", "active", "person"),
		entry("UEXAMPLE02", "Guest", guest+"@demo.getgram.ai", "active", "guest"),
		entry("UEXAMPLE03", "Deactivated", inactive+"@demo.getgram.ai", "deactivated", "person"),
		entry("UEXAMPLE04", "Unknown", "nobody@example.com", "active", "person"),
	)
	require.NoError(t, syncer(f, snapshot).Run(ctx, syncRequest(f, c), nil))

	p := memberRequest()
	p.IncludeDeactivated = true
	page, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	mapped := map[string]string{}
	for _, m := range page.Members {
		if m.Mapping != nil {
			mapped[conv.PtrValOr(m.DisplayName, "")] = m.Mapping.UserID
		}
	}
	require.Equal(t, map[string]string{"Matched": matched}, mapped)

	var member *gen.SlackDirectoryMember
	for _, m := range page.Members {
		if conv.PtrValOr(m.DisplayName, "") == "Matched" {
			member = readMapping(t, ctx, f, m.ID)
		}
	}
	require.NotNil(t, member)
	_, err = f.service.SetMapping(ctx, mappingRequest(member, nil))
	require.NoError(t, err)

	// An administrator's removal is not undone by the next sync.
	require.NoError(t, syncer(f, snapshot).Run(ctx, syncRequest(f, c), nil))
	require.Nil(t, readMapping(t, ctx, f, member.ID).Mapping)
}

func TestDirectoryOrderStaysStableAfterMappingUntilRefreshed(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.NoError(t, syncer(f, directory(
		entry("UEXAMPLE01", "Ada", "ada@example.com", "active", "person"),
		entry("UEXAMPLE02", "Ben", "ben@example.com", "active", "person"),
	)).Run(ctx, syncRequest(f, c), nil))
	page, err := f.service.ListMembers(ctx, memberRequest())
	require.NoError(t, err)
	loaded := page.SortAsOf
	require.Equal(t, []string{"Ada", "Ben"}, names(page))

	_, err = f.service.SetMapping(ctx, mappingRequest(readMapping(t, ctx, f, page.Members[0].ID), new(addPerson(t, ctx, f))))
	require.NoError(t, err)

	p := memberRequest()
	p.SortAsOf = &loaded
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, []string{"Ada", "Ben"}, names(page))
	require.NotNil(t, page.Members[0].Mapping)

	page, err = f.service.ListMembers(ctx, memberRequest())
	require.NoError(t, err)
	require.Equal(t, []string{"Ben", "Ada"}, names(page))
}
