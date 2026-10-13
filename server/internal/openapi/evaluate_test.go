package openapi_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const tokenOrOAuthDoc = `
openapi: 3.1.0
info: {title: Pets, version: 1.0.0}
servers:
  - url: https://api.example.com
security:
  - token: []
  - oauth: [pets:read]
components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
    oauth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes:
            pets:read: Read pets
            pets:write: Write pets
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
      parameters:
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200": {description: ok}
  /pets/{id}:
    get:
      operationId: getPet
      summary: Get a pet
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`

const tokenOnlyDoc = `
openapi: 3.1.0
info: {title: Pets, version: 1.0.1}
servers:
  - url: https://api.example.com
security:
  - token: []
components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
      parameters:
        - name: limit
          in: query
          schema: {type: integer}
      responses:
        "200": {description: ok}
  /pets/{id}:
    get:
      operationId: getPet
      summary: Get a pet
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`

func evaluate(t *testing.T, base, next string) *openapi.DocumentEvaluation {
	t.Helper()

	docInfo := &types.OpenAPIv3DeploymentAsset{
		ID:      "doc",
		AssetID: "asset",
		Name:    "Pets",
		Slug:    "pets",
	}

	var baseBytes []byte
	if base != "" {
		baseBytes = []byte(base)
	}

	tracer := testenv.NewTracerProvider(t).Tracer("test")
	eval, err := openapi.EvaluateDocumentChange(t.Context(), tracer, docInfo, baseBytes, []byte(next))
	require.NoError(t, err)

	return eval
}

func findChange(t *testing.T, eval *openapi.DocumentEvaluation, code string) openapi.EvaluationChange {
	t.Helper()

	for _, c := range eval.Changes {
		if c.Code == code {
			return c
		}
	}

	require.Failf(t, "change not found", "no change with code %q in %+v", code, eval.Changes)
	return openapi.EvaluationChange{}
}

func TestEvaluateDocumentChange_RemovedOAuthIsBreaking(t *testing.T) {
	t.Parallel()

	eval := evaluate(t, tokenOrOAuthDoc, tokenOnlyDoc)

	require.NotNil(t, eval.Base)
	require.Equal(t, 2, eval.Base.ToolCount)
	require.Equal(t, 2, eval.Next.ToolCount)
	require.Equal(t, "`oauth` (pets:read) or `token`", eval.Base.DefaultSecurity)
	require.Equal(t, "`token`", eval.Next.DefaultSecurity)

	removed := findChange(t, eval, "security_scheme_removed")
	require.Equal(t, openapi.EvaluationSeverityBreaking, removed.Severity)
	require.Equal(t, openapi.EvaluationCategorySecurity, removed.Category)
	require.ElementsMatch(t, []string{"pets_list_pets", "pets_get_pet"}, removed.Tools)

	toolSecurity := findChange(t, eval, "tool_security_changed")
	require.Equal(t, openapi.EvaluationSeverityBreaking, toolSecurity.Severity)
	require.Equal(t, "Security changed on 2 tools", toolSecurity.Title)
	require.Contains(t, strings.Join(toolSecurity.Details, "\n"), "No longer accepted: `oauth` (pets:read)")

	defaultSecurity := findChange(t, eval, "default_security_changed")
	require.Equal(t, openapi.EvaluationSeverityWarning, defaultSecurity.Severity)

	require.Equal(t, openapi.EvaluationSeverityBreaking, eval.Changes[0].Severity, "breaking changes sort first")
}

func TestEvaluateDocumentChange_UnchangedDocumentHasNoChanges(t *testing.T) {
	t.Parallel()

	eval := evaluate(t, tokenOrOAuthDoc, tokenOrOAuthDoc)

	require.Empty(t, eval.Changes)
}

func TestEvaluateDocumentChange_NewSourceHasNoBase(t *testing.T) {
	t.Parallel()

	eval := evaluate(t, "", tokenOnlyDoc)

	require.Nil(t, eval.Base)
	require.Equal(t, 2, eval.Next.ToolCount)
	require.Len(t, eval.Next.SecuritySchemes, 1)
	require.Equal(t, "token", eval.Next.SecuritySchemes[0].Key)
	require.Equal(t, []string{"PETS_TOKEN"}, eval.Next.SecuritySchemes[0].EnvVariables)
	require.Empty(t, eval.Changes)
}

func TestEvaluateDocumentChange_ToolsAndInputs(t *testing.T) {
	t.Parallel()

	next := strings.NewReplacer(
		// Remove getPet entirely.
		`  /pets/{id}:
    get:
      operationId: getPet
      summary: Get a pet
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`, `  /owners:
    get:
      operationId: listOwners
      responses:
        "200": {description: ok}
`,
		// Make limit required and add a new required query parameter.
		`        - name: limit
          in: query
          schema: {type: integer}`, `        - name: limit
          in: query
          required: true
          schema: {type: integer}
        - name: species
          in: query
          required: true
          schema: {type: string}`,
		"https://api.example.com", "https://api.example.com/v2",
	).Replace(tokenOrOAuthDoc)

	eval := evaluate(t, tokenOrOAuthDoc, next)

	removed := findChange(t, eval, "tool_removed")
	require.Equal(t, openapi.EvaluationSeverityBreaking, removed.Severity)
	require.Equal(t, []string{"pets_get_pet"}, removed.Tools)

	added := findChange(t, eval, "tool_added")
	require.Equal(t, openapi.EvaluationSeverityInfo, added.Severity)
	require.Equal(t, []string{"pets_list_owners"}, added.Tools)

	inputs := findChange(t, eval, "tool_inputs_changed")
	require.Equal(t, openapi.EvaluationSeverityWarning, inputs.Severity)
	require.ElementsMatch(t, []string{
		"pets_list_pets: query parameter `limit` is now required",
		"pets_list_pets: required query parameter `species` added",
	}, inputs.Details)

	server := findChange(t, eval, "server_url_changed")
	require.Equal(t, openapi.EvaluationSeverityWarning, server.Severity)
	require.Contains(t, server.Details, "After: https://api.example.com/v2")
}

func TestEvaluateDocumentChange_RenamedSchemeChangesEnvVariables(t *testing.T) {
	t.Parallel()

	next := strings.ReplaceAll(tokenOnlyDoc, "token", "apiToken")

	eval := evaluate(t, tokenOnlyDoc, next)

	removed := findChange(t, eval, "security_scheme_removed")
	require.Equal(t, openapi.EvaluationSeverityBreaking, removed.Severity)
	require.Contains(t, removed.Details, "No longer used: PETS_TOKEN")

	added := findChange(t, eval, "security_scheme_added")
	require.Contains(t, added.Details, "New environment variables: PETS_API_TOKEN")
}

func TestEvaluateDocumentChange_OAuthScopeAndFlowChanges(t *testing.T) {
	t.Parallel()

	next := strings.NewReplacer(
		"https://auth.example.com/token", "https://login.example.com/token",
		"            pets:write: Write pets\n", "",
	).Replace(tokenOrOAuthDoc)

	eval := evaluate(t, tokenOrOAuthDoc, next)

	flows := findChange(t, eval, "oauth_flows_changed")
	require.Equal(t, openapi.EvaluationSeverityWarning, flows.Severity)
	require.Contains(t, flows.Details, "`authorizationCode` token URL: https://auth.example.com/token → https://login.example.com/token")
	require.Contains(t, flows.Details, "`authorizationCode` scopes removed: pets:write")
}

func TestEvaluateDocumentChange_InvalidDocument(t *testing.T) {
	t.Parallel()

	docInfo := &types.OpenAPIv3DeploymentAsset{ID: "doc", AssetID: "asset", Name: "Pets", Slug: "pets"}
	tracer := testenv.NewTracerProvider(t).Tracer("test")

	_, err := openapi.EvaluateDocumentChange(t.Context(), tracer, docInfo, nil, []byte("{not: [valid"))
	require.Error(t, err)
}

func TestEvaluateDocumentChange_OptionalInputSchemaChangeIsInfo(t *testing.T) {
	t.Parallel()

	next := strings.Replace(tokenOnlyDoc, `        - name: limit
          in: query
          schema: {type: integer}`, `        - name: limit
          in: query
          schema: {type: integer, maximum: 100}`, 1)

	eval := evaluate(t, tokenOnlyDoc, next)

	schema := findChange(t, eval, "tool_input_schema_changed")
	require.Equal(t, openapi.EvaluationSeverityInfo, schema.Severity)
	require.Equal(t, []string{"pets_list_pets"}, schema.Tools)
	require.Equal(t, []string{"pets_list_pets: query parameter `limit` schema changed"}, schema.Details)
}

const sharedOwnerDoc = `
openapi: 3.1.0
info: {title: Pets, version: 1.0.0}
servers:
  - url: https://api.example.com
paths:
  /pets:
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/NewPet'}
      responses:
        "200": {description: ok}
components:
  schemas:
    NewPet:
      type: object
      properties:
        name: {type: string}
        owner: {$ref: '#/components/schemas/Owner'}
    Owner:
      type: object
      required: [name]
      properties:
        name: {type: string}
`

func TestEvaluateDocumentChange_SharedSchemaRequiredFieldIsWarning(t *testing.T) {
	t.Parallel()

	next := strings.Replace(sharedOwnerDoc, `      required: [name]
      properties:
        name: {type: string}
`, `      required: [name, email]
      properties:
        name: {type: string}
        email: {type: string}
`, 1)

	eval := evaluate(t, sharedOwnerDoc, next)

	inputs := findChange(t, eval, "tool_inputs_changed")
	require.Equal(t, openapi.EvaluationSeverityWarning, inputs.Severity)
	require.Equal(t, []string{"pets_create_pet: body field `owner` now requires `email`"}, inputs.Details)
}

func TestEvaluateDocumentChange_RenamedToolReportsDeployedName(t *testing.T) {
	t.Parallel()

	next := strings.Replace(tokenOnlyDoc, "operationId: listPets", "operationId: fetchPets", 1)

	eval := evaluate(t, tokenOnlyDoc, next)

	renamed := findChange(t, eval, "tool_renamed")
	require.Equal(t, openapi.EvaluationSeverityBreaking, renamed.Severity)
	require.Equal(t, []string{"pets_list_pets"}, renamed.Tools)
}

func TestEvaluateDocumentChange_OptionalAuthIsNotBreaking(t *testing.T) {
	t.Parallel()

	base := strings.Replace(tokenOnlyDoc, "security:\n  - token: []\n", "", 1)
	next := strings.Replace(tokenOnlyDoc, "security:\n  - token: []\n", "security:\n  - {}\n  - token: []\n", 1)

	eval := evaluate(t, base, next)

	security := findChange(t, eval, "tool_security_changed")
	require.Equal(t, openapi.EvaluationSeverityInfo, security.Severity)
}
