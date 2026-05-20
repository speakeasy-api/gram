package collections_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func TestCollectionsService_Audit_Create(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Audited Collection",
		Slug:                 "audited-collection",
		Description:          nil,
		McpRegistryNamespace: "com.example.audited",
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)
	require.Equal(t, "mcp_collection", record.SubjectType)
	require.Equal(t, "Audited Collection", record.SubjectDisplay)
	require.Equal(t, "audited-collection", record.SubjectSlug)
}

func TestCollectionsService_Audit_CreateFailureDoesNotLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)

	_, err = ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Invalid Toolset Collection",
		Slug:                 "invalid-toolset-collection",
		Description:          nil,
		McpRegistryNamespace: "com.example.invalid-toolset",
		Visibility:           "private",
		ToolsetIds:           []string{"not-a-uuid"},
		SessionToken:         nil,
		ApikeyToken:          nil,
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestCollectionsService_Audit_ForbiddenCreateDoesNotLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)

	_, err = ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 "Forbidden Collection",
		Slug:                 "forbidden-collection",
		Description:          nil,
		McpRegistryNamespace: "com.example.forbidden",
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestCollectionsService_Audit_Update(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	collection := createCollection(t, ctx, ti, "Before Update", "before-update", "com.example.before-update")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionUpdate)
	require.NoError(t, err)

	name := "After Update"
	desc := "Updated description"
	visibility := "public"
	updated, err := ti.service.Update(ctx, &gen.UpdatePayload{
		CollectionID: collection.ID,
		Name:         &name,
		Description:  &desc,
		Visibility:   &visibility,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, "After Update", updated.Name)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionUpdate)
	require.NoError(t, err)
	require.Equal(t, "mcp_collection", record.SubjectType)
	require.Equal(t, "After Update", record.SubjectDisplay)
	require.Equal(t, "before-update", record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Before Update", beforeSnapshot["Name"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "After Update", afterSnapshot["Name"])
}

func TestCollectionsService_Audit_Delete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	collection := createCollection(t, ctx, ti, "Delete Me", "delete-me", "com.example.delete-me")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionDelete)
	require.NoError(t, err)

	err = ti.service.Delete(ctx, &gen.DeletePayload{
		CollectionID: collection.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionDelete)
	require.NoError(t, err)
	require.Equal(t, "mcp_collection", record.SubjectType)
	require.Equal(t, "Delete Me", record.SubjectDisplay)
	require.Equal(t, "delete-me", record.SubjectSlug)
}

func TestCollectionsService_Audit_AttachAndDetachServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(t, ctx, ti, "Audited Toolset", "")
	collection := createCollection(t, ctx, ti, "Attach Detach", "attach-detach", "com.example.attach-detach")

	beforeAttachCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionAttachServer)
	require.NoError(t, err)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	afterAttachCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionAttachServer)
	require.NoError(t, err)
	require.Equal(t, beforeAttachCount+1, afterAttachCount)

	attachRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionAttachServer)
	require.NoError(t, err)
	require.Equal(t, "mcp_collection", attachRecord.SubjectType)
	require.Equal(t, "Attach Detach", attachRecord.SubjectDisplay)

	attachMetadata, err := audittest.DecodeAuditData(attachRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, toolset.ID, attachMetadata["toolset_id"])

	beforeDetachCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionDetachServer)
	require.NoError(t, err)

	err = ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	afterDetachCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpCollectionDetachServer)
	require.NoError(t, err)
	require.Equal(t, beforeDetachCount+1, afterDetachCount)

	detachRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionDetachServer)
	require.NoError(t, err)
	require.Equal(t, "mcp_collection", detachRecord.SubjectType)
	require.Equal(t, "Attach Detach", detachRecord.SubjectDisplay)

	detachMetadata, err := audittest.DecodeAuditData(detachRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, toolset.ID, detachMetadata["toolset_id"])
}
