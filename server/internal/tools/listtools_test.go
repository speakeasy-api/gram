package tools_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agen "github.com/speakeasy-api/gram/server/gen/assets"
	dgen "github.com/speakeasy-api/gram/server/gen/deployments"
	tgen "github.com/speakeasy-api/gram/server/gen/templates"
	gen "github.com/speakeasy-api/gram/server/gen/tools"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolsService_ListTools_Success(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment to generate tools
	deployment, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
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

	// Create prompt templates
	template1, err := ti.templates.CreateTemplate(ctx, &tgen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("test-template-1"),
		Prompt:           "Hello {{name}}!",
		Description:      conv.Ptr("A test greeting template"),
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        []string{"assistant"},
		Arguments:        conv.Ptr(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`),
	})
	require.NoError(t, err, "create first template")

	template2, err := ti.templates.CreateTemplate(ctx, &tgen.CreateTemplatePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             types.Slug("test-template-2"),
		Prompt:           "Summarize: {{text}}",
		Description:      conv.Ptr("A summarization template"),
		Engine:           "mustache",
		Kind:             "prompt",
		ToolsHint:        nil,
		Arguments:        conv.Ptr(`{"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]}`),
	})
	require.NoError(t, err, "create second template")

	// Test ListTools
	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "list tools")

	// Verify response structure
	require.NotNil(t, result.Tools, "tools should not be nil")
	require.GreaterOrEqual(t, len(result.Tools), 3, "should have at least 3 tools (1+ HTTP, 2 templates)")

	// Count HTTP tools and prompt templates
	httpToolCount := 0
	templateCount := 0
	templateIDs := map[string]bool{
		template1.Template.ID: true,
		template2.Template.ID: true,
	}

	for _, tool := range result.Tools {
		if tool.HTTPToolDefinition != nil {
			httpToolCount++
			require.NotEmpty(t, tool.HTTPToolDefinition.ID, "tool ID should not be empty")
			require.Equal(t, deployment.Deployment.ID, tool.HTTPToolDefinition.DeploymentID, "deployment ID should match")
			require.NotEmpty(t, tool.HTTPToolDefinition.Name, "tool name should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.HTTPMethod, "HTTP method should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.Path, "path should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.CreatedAt, "created at should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.UpdatedAt, "updated at should not be empty")
		} else if tool.PromptTemplate != nil {
			templateCount++
			require.NotEmpty(t, tool.PromptTemplate.ID, "template ID should not be empty")
			require.True(t, templateIDs[tool.PromptTemplate.ID], "template ID should match one of the created templates")
			require.NotEmpty(t, tool.PromptTemplate.Name, "template name should not be empty")
			require.NotEmpty(t, tool.PromptTemplate.Prompt, "template prompt should not be empty")
			require.NotEmpty(t, tool.PromptTemplate.CreatedAt, "template created at should not be empty")
			require.NotEmpty(t, tool.PromptTemplate.UpdatedAt, "template updated at should not be empty")
		} else {
			t.Fatal("tool has neither HTTPToolDefinition nor PromptTemplate set")
		}
	}

	require.GreaterOrEqual(t, httpToolCount, 1, "should have at least one http tool")
	require.Equal(t, 2, templateCount, "should have exactly 2 prompt templates")
}

func TestToolsService_ListTools_EmptyList(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Test ListTools when no tools exist
	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "should not error when no tools exist")
	require.NotNil(t, result.Tools, "tools should not be nil")
	require.Empty(t, result.Tools, "tools should be empty when no tools exist")
	require.Nil(t, result.NextCursor, "next cursor should be nil for empty results")
}

func TestToolsService_ListTools_WithCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload all available OpenAPI fixtures to ensure we have enough tools for pagination
	fixtures := []string{
		"petstore-valid.yaml",
		"todo-valid.yaml",
		"crm-valid.yaml",
		// "github-valid.yaml",
	}

	var assets []*agen.UploadOpenAPIv3Result
	for i, fixture := range fixtures {
		bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/"+fixture))
		ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ContentType:      "application/x-yaml",
			ContentLength:    int64(bs.Len()),
		}, io.NopCloser(bs))
		require.NoError(t, err, "upload openapi v3 asset %d", i+1)
		assets = append(assets, ares)
	}

	// Create deployment with all assets to generate many tools
	var deploymentAssets []*dgen.AddOpenAPIv3DeploymentAssetForm
	for i, asset := range assets {
		deploymentAssets = append(deploymentAssets, &dgen.AddOpenAPIv3DeploymentAssetForm{
			AssetID: asset.Asset.ID,
			Name:    fmt.Sprintf("doc-%d", i+1),
			Slug:    types.Slug(fmt.Sprintf("doc-%d", i+1)),
		})
	}

	_, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey:   "test-list-tools-cursor",
		Openapiv3Assets:  deploymentAssets,
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
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

	limit := conv.Ptr[int32](4)

	// Get first page - with enough tools we should get a cursor
	firstPage, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            limit,
	})
	require.NoError(t, err, "get first page of tools")
	require.NotNil(t, firstPage.Tools, "first page tools should not be nil")
	require.Len(t, firstPage.Tools, int(*limit), "should have exactly %d tools", *limit)
	require.NotNil(t, firstPage.NextCursor, "should have a next cursor with this many tools")

	// Test pagination with the cursor
	secondPage, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           firstPage.NextCursor,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            limit,
	})
	require.NoError(t, err, "get second page of tools")
	require.NotNil(t, secondPage.Tools, "second page tools should not be nil")

	// Verify the pages contain different tools
	firstPageIDs := make(map[string]bool)
	for _, tool := range firstPage.Tools {
		var id string
		if tool.HTTPToolDefinition != nil {
			id = tool.HTTPToolDefinition.ID
		} else if tool.PromptTemplate != nil {
			id = tool.PromptTemplate.ID
		}
		firstPageIDs[id] = true
	}

	for _, tool := range secondPage.Tools {
		var id string
		if tool.HTTPToolDefinition != nil {
			id = tool.HTTPToolDefinition.ID
		} else if tool.PromptTemplate != nil {
			id = tool.PromptTemplate.ID
		}
		require.False(t, firstPageIDs[id], "second page should not contain tools from first page")
	}
}

func TestToolsService_ListTools_WithDeploymentID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload OpenAPI assets for multiple deployments
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	// Create multiple deployments
	deployment1, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-deployment-1",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create first deployment")

	deployment2, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-deployment-2",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "todo-doc",
				Slug:    "todo-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create second deployment")

	// Test ListTools filtered by first deployment
	result1, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     &deployment1.Deployment.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "list tools for first deployment")
	require.NotNil(t, result1.Tools, "tools should not be nil")

	// Count HTTP tools and verify they belong to first deployment
	httpToolCount := 0
	for _, tool := range result1.Tools {
		if tool.HTTPToolDefinition != nil {
			httpToolCount++
			require.Equal(t, deployment1.Deployment.ID, tool.HTTPToolDefinition.DeploymentID, "all http tools should belong to first deployment")
		}
	}
	require.GreaterOrEqual(t, httpToolCount, 1, "should have at least one http tool")

	// Test ListTools filtered by second deployment
	result2, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     &deployment2.Deployment.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "list tools for second deployment")
	require.NotNil(t, result2.Tools, "tools should not be nil")

	// Count HTTP tools and verify they belong to second deployment
	httpToolCount = 0
	for _, tool := range result2.Tools {
		if tool.HTTPToolDefinition != nil {
			httpToolCount++
			require.Equal(t, deployment2.Deployment.ID, tool.HTTPToolDefinition.DeploymentID, "all http tools should belong to second deployment")
		}
	}
	require.GreaterOrEqual(t, httpToolCount, 1, "should have at least one http tool")
}

func TestToolsService_ListTools_InvalidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Test with invalid cursor UUID
	invalidCursor := "invalid-cursor-uuid"
	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           &invalidCursor,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cursor")
}

func TestToolsService_ListTools_InvalidDeploymentID(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Test with invalid deployment ID UUID
	invalidDeploymentID := "invalid-deployment-uuid"
	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     &invalidDeploymentID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid deployment ID")
}

func TestToolsService_ListTools_Unauthorized(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	_, ti := newTestToolsService(t, assetStorage)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsService_ListTools_ValidCursor(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload OpenAPI asset
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	_, err = ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-valid-cursor",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
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

	// Test with valid cursor (using a valid UUID format)
	validCursor := uuid.New().String()
	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           &validCursor,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "should not error with valid cursor format")
	require.NotNil(t, result.Tools, "tools should not be nil")
}

func TestToolsService_ListTools_VerifyToolFields(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload OpenAPI asset with multiple operations
	bs := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs.Len()),
	}, io.NopCloser(bs))
	require.NoError(t, err, "upload openapi v3 asset")

	// Create deployment
	deployment, err := ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-verify-fields",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
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

	// Test ListTools
	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "list tools")
	require.NotNil(t, result.Tools, "tools should not be nil")

	// Count and verify HTTP tools
	httpToolCount := 0
	for _, tool := range result.Tools {
		if tool.HTTPToolDefinition != nil {
			httpToolCount++
			require.NotEmpty(t, tool.HTTPToolDefinition.ID, "tool ID should not be empty")
			require.Equal(t, deployment.Deployment.ID, tool.HTTPToolDefinition.DeploymentID, "deployment ID should match")
			require.NotEmpty(t, tool.HTTPToolDefinition.ProjectID, "project ID should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.Name, "tool name should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.CanonicalName, "canonical name should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.HTTPMethod, "HTTP method should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.Path, "path should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.CreatedAt, "created at should not be empty")
			require.NotEmpty(t, tool.HTTPToolDefinition.UpdatedAt, "updated at should not be empty")
			require.NotNil(t, tool.HTTPToolDefinition.Openapiv3DocumentID, "openapi document ID should not be nil")
			require.NotNil(t, tool.HTTPToolDefinition.Openapiv3Operation, "openapi operation should not be nil")
			require.NotNil(t, tool.HTTPToolDefinition.SchemaVersion, "schema version should not be nil")
			require.NotEmpty(t, tool.HTTPToolDefinition.Schema, "schema should not be empty")

			// Verify that confirm is a valid value
			require.Contains(t, []string{"", "never", "always", "dangerous"}, *tool.HTTPToolDefinition.Confirm, "confirm should be a valid value")
		}
	}
	require.GreaterOrEqual(t, httpToolCount, 1, "should have at least one http tool")
}

func TestToolsService_ListTools_MultipleDeployments(t *testing.T) {
	t.Parallel()

	assetStorage := assetstest.NewTestBlobStore(t)
	ctx, ti := newTestToolsService(t, assetStorage)

	// Upload multiple OpenAPI assets
	bs1 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/petstore-valid.yaml"))
	ares1, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs1.Len()),
	}, io.NopCloser(bs1))
	require.NoError(t, err, "upload first openapi v3 asset")

	bs2 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/todo-valid.yaml"))
	ares2, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs2.Len()),
	}, io.NopCloser(bs2))
	require.NoError(t, err, "upload second openapi v3 asset")

	bs3 := bytes.NewBuffer(testenv.ReadFixture(t, "fixtures/crm-valid.yaml"))
	ares3, err := ti.assets.UploadOpenAPIv3(ctx, &agen.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/x-yaml",
		ContentLength:    int64(bs3.Len()),
	}, io.NopCloser(bs3))
	require.NoError(t, err, "upload third openapi v3 asset")

	// Create multiple deployments
	deployments := make([]*dgen.CreateDeploymentResult, 3)

	deployments[0], err = ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-multiple-first",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares1.Asset.ID,
				Name:    "petstore-doc",
				Slug:    "petstore-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create first deployment")

	deployments[1], err = ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-multiple-second",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares2.Asset.ID,
				Name:    "todo-doc",
				Slug:    "todo-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create second deployment")

	deployments[2], err = ti.deployments.CreateDeployment(ctx, &dgen.CreateDeploymentPayload{
		IdempotencyKey: "test-list-tools-multiple-third",
		Openapiv3Assets: []*dgen.AddOpenAPIv3DeploymentAssetForm{
			{
				AssetID: ares3.Asset.ID,
				Name:    "crm-doc",
				Slug:    "crm-doc",
			},
		},
		Functions:        []*dgen.AddFunctionsForm{},
		Packages:         []*dgen.AddDeploymentPackageForm{},
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		GithubRepo:       nil,
		GithubPr:         nil,
		GithubSha:        nil,
		ExternalID:       nil,
		ExternalURL:      nil,
	})
	require.NoError(t, err, "create third deployment")

	// List all tools
	result, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		Cursor:           nil,
		DeploymentID:     nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Limit:            nil,
	})
	require.NoError(t, err, "list all tools")
	require.NotNil(t, result.Tools, "tools should not be nil")

	// Count HTTP tools and verify they belong to the last deployment
	httpToolCount := 0
	for _, tool := range result.Tools {
		if tool.HTTPToolDefinition != nil {
			httpToolCount++
			require.Equal(t, deployments[2].Deployment.ID, tool.HTTPToolDefinition.DeploymentID, "all http tools should belong to the last deployment")
		}
	}
	require.GreaterOrEqual(t, httpToolCount, 3, "should have at least 3 http tools")
}
