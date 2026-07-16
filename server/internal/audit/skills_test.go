package audit

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillSnapshotSerializesAllFieldsWithoutSummaryOrMutation(t *testing.T) {
	t.Parallel()

	archivedAt := "2026-07-15T12:00:00Z"
	snapshot := &SkillSnapshot{
		ID:              "skill-id",
		ProjectID:       "project-id",
		Name:            "incident-response",
		DisplayName:     "Incident Response",
		SourceKind:      "manual",
		Classification:  "custom",
		LatestVersionID: "version-id",
		VersionCount:    3,
		CreatedAt:       "2026-07-14T12:00:00Z",
		UpdatedAt:       "2026-07-15T11:00:00Z",
		ArchivedAt:      &archivedAt,
	}
	original := *snapshot

	payload, err := marshalAuditPayload(snapshot)
	require.NoError(t, err)
	require.Equal(t, original, *snapshot)
	require.NotContains(t, string(payload), "Summary")
	require.JSONEq(t, `{
		"ID":"skill-id",
		"ProjectID":"project-id",
		"Name":"incident-response",
		"DisplayName":"Incident Response",
		"SourceKind":"manual",
		"Classification":"custom",
		"LatestVersionID":"version-id",
		"VersionCount":3,
		"CreatedAt":"2026-07-14T12:00:00Z",
		"UpdatedAt":"2026-07-15T11:00:00Z",
		"ArchivedAt":"2026-07-15T12:00:00Z"
	}`, string(payload))
}

func TestSkillSnapshotPreservesArchiveTransitionWithoutMutation(t *testing.T) {
	t.Parallel()

	before := &SkillSnapshot{
		ID:              "skill-id",
		ProjectID:       "project-id",
		Name:            "incident-response",
		DisplayName:     "Incident Response",
		SourceKind:      "manual",
		Classification:  "custom",
		LatestVersionID: "version-id",
		VersionCount:    3,
		CreatedAt:       "2026-07-14T12:00:00Z",
		UpdatedAt:       "2026-07-15T11:00:00Z",
		ArchivedAt:      nil,
	}
	archivedAt := "2026-07-15T12:00:00Z"
	after := *before
	after.UpdatedAt = archivedAt
	after.ArchivedAt = &archivedAt
	beforeOriginal := *before
	afterOriginal := after

	beforePayload, err := marshalAuditPayload(before)
	require.NoError(t, err)
	afterPayload, err := marshalAuditPayload(&after)
	require.NoError(t, err)
	require.Equal(t, beforeOriginal, *before)
	require.Equal(t, afterOriginal, after)

	var beforeData map[string]any
	require.NoError(t, json.Unmarshal(beforePayload, &beforeData))
	var afterData map[string]any
	require.NoError(t, json.Unmarshal(afterPayload, &afterData))
	require.Contains(t, beforeData, "ArchivedAt")
	require.Nil(t, beforeData["ArchivedAt"])
	require.Equal(t, archivedAt, afterData["ArchivedAt"])
	require.Equal(t, archivedAt, afterData["UpdatedAt"])
	require.NotContains(t, beforeData, "Summary")
	require.NotContains(t, afterData, "Summary")
}

func TestSkillDistributionSnapshotIsContentFree(t *testing.T) {
	t.Parallel()

	pin := "version-id"
	pluginID := "plugin-id"
	revokedAt := "2026-07-16T12:00:00Z"
	snapshot := &SkillDistributionSnapshot{
		ID:              "distribution-id",
		ProjectID:       "project-id",
		SkillID:         "skill-id",
		PluginID:        &pluginID,
		PinnedVersionID: &pin,
		Channel:         "plugin",
		CreatedByUserID: "user-id",
		RevokedAt:       &revokedAt,
		CreatedAt:       "2026-07-16T10:00:00Z",
		UpdatedAt:       "2026-07-16T12:00:00Z",
	}
	original := *snapshot

	payload, err := marshalAuditPayload(snapshot)
	require.NoError(t, err)
	require.Equal(t, original, *snapshot)
	require.NotContains(t, string(payload), "Summary")
	require.NotContains(t, string(payload), "Content")
	require.JSONEq(t, `{
		"ID":"distribution-id",
		"ProjectID":"project-id",
		"SkillID":"skill-id",
		"PluginID":"plugin-id",
		"PinnedVersionID":"version-id",
		"Channel":"plugin",
		"CreatedByUserID":"user-id",
		"RevokedAt":"2026-07-16T12:00:00Z",
		"CreatedAt":"2026-07-16T10:00:00Z",
		"UpdatedAt":"2026-07-16T12:00:00Z"
	}`, string(payload))
}
