package skills_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

type concurrentCreateResult struct {
	result *gen.RecordSkillResult
	err    error
}

type concurrentDistributionResult struct {
	id  string
	err error
}

func TestSkillsConcurrentDistributionCreatesOneActiveRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "concurrent-distribution", "Valid distribution.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "concurrent-plugin")
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	const requests = 8
	start := make(chan struct{})
	results := make(chan concurrentDistributionResult, requests)

	for range requests {
		go func() {
			<-start
			distribution, distributeErr := ti.service.Distribute(ctx, &gen.DistributePayload{
				ID: created.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: nil,
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
			})
			if distributeErr != nil {
				results <- concurrentDistributionResult{id: "", err: distributeErr}
				return
			}
			results <- concurrentDistributionResult{id: distribution.ID, err: nil}
		}()
	}
	close(start)

	var distributionID string
	for range requests {
		result := <-results
		require.NoError(t, result.err)
		if distributionID == "" {
			distributionID = result.id
		}
		require.Equal(t, distributionID, result.id)
	}
	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Distributions, 1)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestSkillsConcurrentAssistantDistributionCreatesOneActiveRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "concurrent-assistant-distribution", "Valid distribution.")
	assistant := createAssistant(t, ctx, ti, ti.projectID, "Concurrent assistant")
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopeProjectWrite, ti.projectID.String()),
	)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	const requests = 8
	start := make(chan struct{})
	results := make(chan concurrentDistributionResult, requests)

	for range requests {
		go func() {
			<-start
			distribution, distributeErr := ti.service.Distribute(ctx, &gen.DistributePayload{
				ID: created.Skill.ID, AssistantID: new(assistant.ID.String()), PinnedVersionID: nil,
				SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
			})
			if distributeErr != nil {
				results <- concurrentDistributionResult{id: "", err: distributeErr}
				return
			}
			results <- concurrentDistributionResult{id: distribution.ID, err: nil}
		}()
	}
	close(start)

	var distributionID string
	for range requests {
		result := <-results
		require.NoError(t, result.err)
		if distributionID == "" {
			distributionID = result.id
		}
		require.Equal(t, distributionID, result.id)
	}
	details, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.EqualValues(t, 1, details.AssistantCount)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestSkillsConcurrentSameNameAndHashCreatesOneVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	content := skillManifest("concurrent-same", "Same content.", "body")
	const uploads = 8
	start := make(chan struct{})
	results := make(chan concurrentCreateResult, uploads)

	for range uploads {
		go func() {
			<-start
			result, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			results <- concurrentCreateResult{result: result, err: err}
		}()
	}
	close(start)

	var skillID, versionID string
	for range uploads {
		call := <-results
		require.NoError(t, call.err)
		require.NotNil(t, call.result)
		if skillID == "" {
			skillID = call.result.Skill.ID
			versionID = call.result.Version.ID
		}
		require.Equal(t, skillID, call.result.Skill.ID)
		require.Equal(t, versionID, call.result.Version.ID)
	}

	listed, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Skills, 1)
	require.Equal(t, int64(1), listed.Skills[0].VersionCount)
	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: skillID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, versions.Versions, 1)
}

func TestSkillsConcurrentSameNameDistinctHashesCreateEveryVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	const uploads = 8
	start := make(chan struct{})
	results := make(chan concurrentCreateResult, uploads)

	for i := range uploads {
		content := skillManifest("concurrent-distinct", fmt.Sprintf("Description %d.", i), fmt.Sprintf("body %d", i))
		go func() {
			<-start
			result, err := ti.service.Create(ctx, &gen.CreatePayload{Content: content, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			results <- concurrentCreateResult{result: result, err: err}
		}()
	}
	close(start)

	skillIDs := make(map[string]struct{}, uploads)
	versionIDs := make(map[string]struct{}, uploads)
	hashes := make(map[string]struct{}, uploads)
	for range uploads {
		call := <-results
		require.NoError(t, call.err)
		require.NotNil(t, call.result)
		skillIDs[call.result.Skill.ID] = struct{}{}
		versionIDs[call.result.Version.ID] = struct{}{}
		hashes[call.result.Version.CanonicalSha256] = struct{}{}
	}
	require.Len(t, skillIDs, 1)
	require.Len(t, versionIDs, uploads)
	require.Len(t, hashes, uploads)

	var skillID string
	for id := range skillIDs {
		skillID = id
	}
	versions, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: skillID, Cursor: nil, Limit: uploads + 1, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, versions.Versions, uploads)
}
