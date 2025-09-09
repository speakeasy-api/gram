package deployments_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	pkggen "github.com/speakeasy-api/gram/server/gen/packages"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestDeploymentsService_CreateDeployment(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)

	ctx, ti := newTestDeploymentService(t, assetStorage)

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))

	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-random-idempotency-key",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "test-doc",
				Slug:    "test-doc",
			},
		},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.Len(t, tools, 5, "expected 5 tools")

	t.Run("tool names", func(t *testing.T) {
		t.Parallel()

		names := lo.Map(tools, func(t testrepo.HttpToolDefinition, _ int) string {
			return t.Name
		})
		require.ElementsMatch(t, names, []string{
			"test_doc_get_todos",
			"test_doc_create_todo",
			"test_doc_get_todo_by_id",
			"test_doc_update_todo",
			"test_doc_delete_todo",
		}, "mismatched tool names")
	})

	t.Run("tool attributes", func(t *testing.T) {
		t.Parallel()

		name := "test_doc_get_todo_by_id"

		tool, ok := lo.Find(tools, func(t testrepo.HttpToolDefinition) bool {
			return t.Name == name
		})

		require.True(t, ok, "tool %s not found", name)
		require.Equal(t, "Get a todo by ID", tool.Summary)
		require.Equal(t, "Retrieve a specific todo item by its ID", tool.Description)
		require.Equal(t, "getTodoById", tool.Openapiv3Operation.String)
		require.Equal(t, "GET", tool.HttpMethod)
		require.Equal(t, "/todos/{id}", tool.Path)
		require.JSONEq(t, `{"type": "object", "required": ["pathParameters"], "properties": {"pathParameters": {"type": "object", "required": ["id"], "properties": {"id": {"type": "string", "format": "uuid", "description": "The ID of the todo to retrieve"}}, "additionalProperties": false}}, "additionalProperties": false}`, string(tool.Schema))
		require.JSONEq(t, `[{"ApiKeyAuth": []}, {"BearerAuth": []}]`, string(tool.Security))
		require.Empty(t, tool.Tags, "tags are not empty")
	})
}

func TestDeploymentsService_CreateDeployment_Idempotency(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)

	ctx, ti := newTestDeploymentService(t, assetStorage)

	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	var idmap sync.Map
	var eg errgroup.Group

	for range 5 {
		eg.Go(func() error {
			dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
				ApikeyToken:      nil,
				SessionToken:     nil,
				ProjectSlugInput: nil,
				IdempotencyKey:   "idempotency-key",
				GithubRepo:       nil,
				GithubPr:         nil,
				GithubSha:        nil,
				ExternalID:       nil,
				ExternalURL:      nil,
				Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
					{
						AssetID: ares.Asset.ID,
						Name:    "test-doc",
						Slug:    "test-doc",
					},
				},
				Packages: []*gen.AddDeploymentPackageForm{},
			})
			if err != nil {
				return fmt.Errorf("create deployment: %w", err)
			}

			idmap.Store(dep.Deployment.ID, struct{}{})

			return nil
		})
	}

	require.NoError(t, eg.Wait(), "create deployments concurrently")

	createdIDs := []string{}

	idmap.Range(func(key, value any) bool {
		k, ok := key.(string)
		require.True(t, ok, "key is not a string")

		createdIDs = append(createdIDs, k)

		return true
	})

	require.Len(t, createdIDs, 1, "expected 1 deployment")

	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentTools(ctx, uuid.MustParse(createdIDs[0]))
	require.NoError(t, err, "list deployment tools")
	require.Len(t, tools, 5, "expected 5 tools")
	names := lo.Map(tools, func(t testrepo.HttpToolDefinition, _ int) string {
		return t.Name
	})
	require.ElementsMatch(t, names, []string{
		"test_doc_get_todos",
		"test_doc_create_todo",
		"test_doc_get_todo_by_id",
		"test_doc_update_todo",
		"test_doc_delete_todo",
	}, "mismatched tool names")
}

func TestCreateDeployment_CreateDeployment_Validation(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestDeploymentService(t, assetStorage)

	t.Run("no assets or packages", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey:   "test-key",
			Openapiv3Assets:  []*gen.AddOpenAPIv3DeploymentAssetForm{},
			Packages:         []*gen.AddDeploymentPackageForm{},
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			GithubRepo:       nil,
			GithubPr:         nil,
			GithubSha:        nil,
			ExternalID:       nil,
			ExternalURL:      nil,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "at least one asset or package is required")
	})

	t.Run("invalid asset ID", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey: "test-key",
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
				{
					AssetID: "invalid-uuid",
					Name:    "test-doc",
					Slug:    "test-doc",
				},
			},
			Packages:         []*gen.AddDeploymentPackageForm{},
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			GithubRepo:       nil,
			GithubPr:         nil,
			GithubSha:        nil,
			ExternalID:       nil,
			ExternalURL:      nil,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing asset id")
	})

	t.Run("invalid package version", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey:  "test-key",
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
			Packages: []*gen.AddDeploymentPackageForm{
				{
					Name:    "test-package",
					Version: conv.Ptr("invalid-version"),
				},
			},
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			GithubRepo:       nil,
			GithubPr:         nil,
			GithubSha:        nil,
			ExternalID:       nil,
			ExternalURL:      nil,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "error parsing semver")
	})

	t.Run("circular package dependency", func(t *testing.T) {
		t.Parallel()

		bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
		ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ContentType:      "application/x-yaml",
			ContentLength:    int64(bs.Len()),
		}, io.NopCloser(bs))
		require.NoError(t, err, "upload openapi v3 asset")

		dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey: "test-random-idempotency-key",
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
				{
					AssetID: ares.Asset.ID,
					Name:    "test-doc",
					Slug:    "test-doc",
				},
			},
			Packages:         []*gen.AddDeploymentPackageForm{},
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			GithubRepo:       nil,
			GithubPr:         nil,
			GithubSha:        nil,
			ExternalID:       nil,
			ExternalURL:      nil,
		})
		require.NoError(t, err, "create deployment")
		require.Equal(t, "completed", dep.Deployment.Status, "deployment status is not completed")

		pkg, err := ti.packages.CreatePackage(ctx, &pkggen.CreatePackagePayload{
			Name:             "test-package",
			Title:            "Test Package",
			Summary:          "Test Package Summary",
			Description:      nil,
			URL:              nil,
			Keywords:         nil,
			ImageAssetID:     nil,
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err, "create package")
		require.NotNil(t, pkg, "package is nil")

		ver, err := ti.packages.Publish(ctx, &pkggen.PublishPayload{
			ProjectSlugInput: nil,
			ApikeyToken:      nil,
			SessionToken:     nil,
			Name:             "test-package",
			Version:          "0.0.1",
			DeploymentID:     dep.Deployment.ID,
			Visibility:       "public",
		})
		require.NoError(t, err, "publish package")
		require.NotNil(t, ver, "package version is nil")

		dep, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
			IdempotencyKey:  "test-random-idempotency-key",
			Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{},
			Packages: []*gen.AddDeploymentPackageForm{
				{
					Name:    "test-package",
					Version: conv.Ptr(ver.Version.Semver),
				},
			},
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			GithubRepo:       nil,
			GithubPr:         nil,
			GithubSha:        nil,
			ExternalID:       nil,
			ExternalURL:      nil,
		})
		require.Contains(t, err.Error(), "cannot add package to its own project: test-package")
		require.Nil(t, dep, "deployment is not nil")
	})
}

func TestDeploymentsService_CreateDeployment_NoDuplicateToolsWithMixedDocuments(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)

	ctx, ti := newTestDeploymentService(t, assetStorage)

	validDoc := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	validAsset, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(validDoc.Len()),
	}, io.NopCloser(validDoc))
	require.NoError(t, err, "upload valid openapi v3 asset")

	invalidDoc := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-invalid.yaml"))
	invalidAsset, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(invalidDoc.Len()),
	}, io.NopCloser(invalidDoc))
	require.NoError(t, err, "upload invalid openapi v3 asset")

	dep, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{
		IdempotencyKey: "test-no-duplicate-tools-key",
		Openapiv3Assets: []*gen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: validAsset.Asset.ID,
				Name:    "valid-doc",
				Slug:    "valid-doc",
			},
			{
				AssetID: invalidAsset.Asset.ID,
				Name:    "invalid-doc",
				Slug:    "invalid-doc",
			},
		},
		Packages:         []*gen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create deployment with mixed documents")

	require.NotEqual(t, uuid.Nil.String(), dep.Deployment.ID, "deployment ID is nil")
	require.Equal(t, "failed", dep.Deployment.Status, "deployment should fail due to invalid document")

	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")

	t.Run("no duplicate tools", func(t *testing.T) {
		t.Parallel()

		toolNames := lo.Map(tools, func(t testrepo.HttpToolDefinition, _ int) string {
			return t.Name
		})

		uniqueNames := lo.Uniq(toolNames)
		require.Len(t, toolNames, len(uniqueNames), "found duplicate tool names: %v", toolNames)
	})

	t.Run("no tools from invalid document", func(t *testing.T) {
		t.Parallel()

		for _, tool := range tools {
			require.NotContains(t, tool.Name, "invalid_doc", "tool %s should not be from invalid document", tool.Name)
		}

		invalidDocTools := lo.Filter(tools, func(t testrepo.HttpToolDefinition, _ int) bool {
			return strings.Contains(t.Name, "invalid_doc")
		})
		require.Empty(t, invalidDocTools, "should have no tools from invalid document")
	})

	t.Run("valid document processed before failure", func(t *testing.T) {
		t.Parallel()

		validDocTools := lo.Filter(tools, func(t testrepo.HttpToolDefinition, _ int) bool {
			return strings.Contains(t.Name, "valid_doc")
		})

		if len(validDocTools) > 0 {
			expectedTools := []string{
				"valid_doc_get_todos",
				"valid_doc_create_todo",
				"valid_doc_get_todo_by_id",
				"valid_doc_update_todo",
				"valid_doc_delete_todo",
			}

			validToolNames := lo.Map(validDocTools, func(t testrepo.HttpToolDefinition, _ int) string {
				return t.Name
			})

			require.ElementsMatch(t, validToolNames, expectedTools, "expected all tools from valid document")
		}
	})
}
