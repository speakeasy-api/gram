package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// leakedDatabaseText is what a database error carries that a caller must never
// see: driver and pool messages, SQL, and relation and constraint names.
var leakedDatabaseText = []string{"pool", "closed", "sql", "pgx", "postgres", "relation", "constraint"}

// driverErrorText is the narrower set checked in a successful result, whose
// legitimate fields can hold a word like "closed" or "sql": only what a
// database error carries and a product value would not.
var driverErrorText = []string{"closed pool", "pgx", "sqlstate"}

func requireNoDatabaseText(t *testing.T, tool, text string) {
	t.Helper()
	requireNoneOf(t, tool, text, leakedDatabaseText)
}

func requireNoneOf(t *testing.T, tool, text string, leaked []string) {
	t.Helper()
	lowered := strings.ToLower(text)
	for _, term := range leaked {
		require.NotContains(t, lowered, term, "%s returned database error text to its caller: %s", tool, text)
	}
}

// invokeWithValidArguments calls a descriptor with arguments built from its
// own input schema, so the call reaches the handler rather than stopping at
// argument validation. Each override replaces a generated field the schema
// asked for — a real project, say — and is ignored where it did not.
func invokeWithValidArguments(t *testing.T, ctx context.Context, descriptor Descriptor, overrides map[string]any) (any, error) {
	t.Helper()
	arguments := sampleArguments(t, descriptor)
	for field, value := range overrides {
		if _, ok := arguments[field]; ok {
			arguments[field] = value
		}
	}
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	output, err := descriptor.Invoke(ctx, encoded)
	if err != nil {
		require.NotContains(t, err.Error(), "arguments do not match the tool schema", "%s: the generated arguments %s did not reach the handler", descriptor.Name, encoded)
	}
	return output, err
}

// sampleArguments builds a value for every required field of a tool's input
// schema. Booleans are true, so a confirmed write takes its write path rather
// than stopping at a preview.
func sampleArguments(t *testing.T, descriptor Descriptor) map[string]any {
	t.Helper()
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema), descriptor.Name)
	if object, ok := sampleSchemaValue(&schema, "").(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func sampleSchemaValue(schema *jsonschema.Schema, name string) any {
	if schema == nil {
		return nil
	}
	if schema.Const != nil {
		return *schema.Const
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}
	if schema.Type == "" && len(schema.Types) == 0 && len(schema.Properties) == 0 {
		for _, branches := range [][]*jsonschema.Schema{schema.OneOf, schema.AnyOf} {
			for _, branch := range branches {
				if value := sampleSchemaValue(branch, name); value != nil {
					return value
				}
			}
		}
	}

	kind := schema.Type
	if kind == "" {
		for _, candidate := range schema.Types {
			if candidate != "null" {
				kind = candidate
				break
			}
		}
	}
	if kind == "" && len(schema.Properties) > 0 {
		kind = "object"
	}
	switch kind {
	case "object":
		object := map[string]any{}
		for _, required := range schema.Required {
			object[required] = sampleSchemaValue(schema.Properties[required], required)
		}
		// An object that also constrains itself with oneOf/anyOf — "name the
		// project by ID or by slug", or one shape per policy type — satisfies
		// its first alternative.
		for _, branches := range [][]*jsonschema.Schema{schema.OneOf, schema.AnyOf} {
			if len(branches) == 0 {
				continue
			}
			branch := branches[0]
			properties := schema.Properties
			if len(branch.Properties) > 0 {
				properties = branch.Properties
			}
			for _, required := range branch.Required {
				if _, ok := object[required]; !ok {
					object[required] = sampleSchemaValue(properties[required], required)
				}
			}
		}
		if schema.MinProperties != nil {
			for _, property := range slices.Sorted(maps.Keys(schema.Properties)) {
				if len(object) >= *schema.MinProperties {
					break
				}
				object[property] = sampleSchemaValue(schema.Properties[property], property)
			}
		}
		return object
	case "array":
		count := 1
		if schema.MinItems != nil {
			count = max(count, *schema.MinItems)
		}
		items := make([]any, 0, count)
		for range count {
			items = append(items, sampleSchemaValue(schema.Items, name))
		}
		return items
	case "boolean":
		return true
	case "integer", "number":
		value := 1
		if schema.Minimum != nil && float64(value) < *schema.Minimum {
			value = int(*schema.Minimum)
		}
		if schema.Maximum != nil && float64(value) > *schema.Maximum {
			value = int(*schema.Maximum)
		}
		return value
	default:
		return sampleString(schema, name)
	}
}

func sampleString(schema *jsonschema.Schema, name string) string {
	var value string
	switch {
	case schema.Format == "uri" || strings.HasSuffix(name, "url"):
		value = "https://mcp.example.com/mcp"
	case schema.Format == "date-time" || strings.HasSuffix(name, "_at") || name == "since" || name == "until" || name == "from" || name == "to":
		value = "2026-01-01T00:00:00Z"
	case schema.Format == "email" || strings.Contains(name, "email"):
		value = "member@example.com"
	default:
		value = uuid.NewString()
	}
	if schema.MaxLength != nil && len(value) > *schema.MaxLength {
		value = value[:*schema.MaxLength]
	}
	if schema.MinLength != nil {
		for len(value) < *schema.MinLength {
			value += "a"
		}
	}
	return value
}

// The backstop runs on every failure path, including the one where nothing
// was composed. Every tool in a registry built with no services at all must
// still answer — with a refusal, never a panic and never raw error text.
func TestEveryToolOnANotComposedServiceRefusesWithoutPanicking(t *testing.T) {
	t.Parallel()

	for name, reader := range map[string]Reader{
		"no reader":           nil,
		"reader with no pool": NewPostgresReader(testenv.NewLogger(t), nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, registrar := newServerWithRiskMutations(reader, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{}, nil, nil, nil, nil)
			// No logger either: the backstop must not read one it was not given.
			ctx := ContextWithPrincipal(t.Context(), testPrincipal())
			descriptors := registrar.Descriptors()
			require.NotEmpty(t, descriptors)
			for _, descriptor := range descriptors {
				t.Run(descriptor.Name, func(t *testing.T) {
					t.Parallel()
					require.NotPanics(t, func() {
						_, err := invokeWithValidArguments(t, ctx, descriptor, nil)
						if err == nil {
							return
						}
						safe, ok := recognisedToolError(err)
						require.True(t, ok, "returned an error that is not a refusal: %v", err)
						requireNoDatabaseText(t, descriptor.Name, safe.Error())
					})
				})
			}
		})
	}
}

func backstopRegistrar(logger *slog.Logger, err error) *Registrar {
	registrar := newRegistrar(newTestMCPServer())
	registrar.withLogger(logger)
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})
	addTool(registrar, &mcp.Tool{Name: "fails", Description: "Fails."},
		ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeNone},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, err
		})
	return registrar
}

func invokeFailingTool(t *testing.T, registrar *Registrar) error {
	t.Helper()
	_, err := registrar.Descriptors()[0].Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), nil)
	return err
}

func TestBackstopReplacesAnUnclassifiedErrorAndLogsItsCause(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	err := invokeFailingTool(t, backstopRegistrar(slog.New(slog.NewTextHandler(&logs, nil)), fmt.Errorf("resolve project: %w", errors.New("closed pool"))))

	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, "an unclassified error becomes a refusal: %v", err)
	require.Equal(t, unavailableCode, refusal.Code)
	require.Contains(t, refusal.Payload, unavailableCode)
	requireNoDatabaseText(t, "fails", refusal.Payload)
	require.Contains(t, logs.String(), "resolve project: closed pool", "the log keeps the full cause, not the sanitized message")
}

func TestBackstopWithoutALoggerStillRefuses(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		refusal, ok := errors.AsType[*ToolRefusalError](invokeFailingTool(t, backstopRegistrar(nil, errors.New("closed pool"))))
		require.True(t, ok)
		require.Equal(t, unavailableCode, refusal.Code)
	})
}

// A JSON-RPC error is not vouched for by its type: a remote MCP server or the
// SDK authors its message and data, so it is unclassified like any other.
func TestBackstopDoesNotForwardAJSONRPCErrorsText(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	remote := &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: `relation "projects" does not exist`, Data: json.RawMessage(`"closed pool"`)}
	err := invokeFailingTool(t, backstopRegistrar(slog.New(slog.NewTextHandler(&logs, nil)), fmt.Errorf("probe upstream: %w", remote)))

	refusal, ok := errors.AsType[*ToolRefusalError](err)
	require.True(t, ok, "a JSON-RPC error becomes the generic refusal: %v", err)
	require.Equal(t, unavailableCode, refusal.Code)
	requireNoDatabaseText(t, "fails", refusal.Payload)
	require.Contains(t, logs.String(), "does not exist")

	// The SDK would otherwise send a handler's JSON-RPC error to the external
	// client as a protocol error carrying its message and data.
	require.Contains(t, requireExternalBackstopRefusal(t, remote), "does not exist")
}

// requireExternalBackstopRefusal calls a tool failing with cause over the
// external endpoint, asserts the client got the generic refusal as a tool
// result, and returns what the server logged.
func requireExternalBackstopRefusal(t *testing.T, cause error) string {
	t.Helper()

	var logs bytes.Buffer
	registrar := backstopRegistrar(slog.New(slog.NewTextHandler(&logs, nil)), cause)
	bindExternalTestPrincipal(registrar.server)

	result, err := connectTestClient(t, registrar.server).CallTool(t.Context(), &mcp.CallToolParams{Name: "fails", Arguments: map[string]any{}})
	require.NoError(t, err, "the external client gets a tool result, not the handler's error")
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, unavailableCode)
	requireNoDatabaseText(t, "fails", text.Text)
	return logs.String()
}

// A recognised error keeps its meaning: a tool's own refusal and an
// authorization denial pass through, unwrapped from any context that was
// added around them, and the package's bare sentinels keep their identity.
func TestBackstopPassesRecognisedErrorsThrough(t *testing.T) {
	t.Parallel()

	own := &ToolRefusalError{Code: "slug_taken", Payload: `{"code":"slug_taken"}`}
	denied := &ExternalAuthorizationError{RequiredScope: "org:admin", RequestAccessURL: "", cause: nil}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"refusal", own, own},
		{"wrapped refusal", fmt.Errorf("pgx: closed pool: %w", own), own},
		{"denial", denied, denied},
		{"wrapped denial", fmt.Errorf("closed pool: %w", denied), denied},
		{"unauthorized", ErrUnauthorized, ErrUnauthorized},
		{"unavailable", ErrUnavailable, ErrUnavailable},
	} {
		var logs bytes.Buffer
		err := invokeFailingTool(t, backstopRegistrar(slog.New(slog.NewTextHandler(&logs, nil)), tc.err))
		require.Same(t, tc.want, err, tc.name)
		require.Empty(t, logs.String(), "%s: a recognised error is not an unclassified failure", tc.name)
	}

	// A sentinel wrapped around a cause carries that cause's text, so only the
	// bare sentinel is trusted.
	refusal, ok := errors.AsType[*ToolRefusalError](invokeFailingTool(t, backstopRegistrar(nil, fmt.Errorf("%w: lookup session: closed pool", ErrUnavailable))))
	require.True(t, ok)
	requireNoDatabaseText(t, "fails", refusal.Payload)
}

// The external endpoint applies the same backstop: the MCP SDK would
// otherwise return the handler's error to the client as the result text.
func TestBackstopCoversTheExternalEndpoint(t *testing.T) {
	t.Parallel()

	logs := requireExternalBackstopRefusal(t, errors.New(`pgx: relation "projects" does not exist: closed pool`))
	require.Contains(t, logs, `relation \"projects\" does not exist`)
}

// A resource read is authorized live, as a tool call is, so the same backstop
// keeps an authorization outage out of the error the client sees.
func TestBackstopCoversResourceReads(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	registrar := newRegistrar(newTestMCPServer())
	registrar.withLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	registrar.withExternalAuthorizer(failingExternalCallAuthorizer{err: errors.New("load grants: closed pool")})
	bindExternalTestPrincipal(registrar.server)
	addResource(registrar, &mcp.Resource{URI: "gram://backstop", Name: "backstop", MIMEType: "text/markdown"},
		ResourceMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences},
		func(context.Context) (string, error) { return "never read", nil })

	_, err := connectTestClient(t, registrar.server).ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "gram://backstop"})
	require.Error(t, err)
	require.Contains(t, err.Error(), unavailableCode)
	requireNoDatabaseText(t, "read", err.Error())
	require.Contains(t, logs.String(), "load grants: closed pool")
}

type failingExternalCallAuthorizer struct {
	allowExternalCallAuthorizer
	err error
}

func (a failingExternalCallAuthorizer) AuthorizeExternalCall(context.Context, Principal, ExternalAuthorization) error {
	return a.err
}
