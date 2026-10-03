package platformmcp

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// sampleWireValues supplies a valid sample for a type whose JSON form cannot be
// derived from its Go shape, because it has its own MarshalJSON.
var sampleWireValues = map[reflect.Type]any{
	reflect.TypeFor[SubjectCount](): NewSubjectCount(25),
}

// A client keeps the output schema it fetched when it connected and validates
// every later result against it, so the schema a tool advertises must accept
// whatever its result type serializes to, with every field populated. Asserted
// against a real session's tools/list, for every tool that session lists, so a
// result type can no longer drift from the schema its clients hold.
func TestEveryAdvertisedOutputSchemaAcceptsAPopulatedResult(t *testing.T) {
	t.Parallel()

	// Both registration branches: with no reader the deployment substitutes
	// "not switched on" stubs for the tools that need one, and a reader with no
	// pool registers the live handlers, whose result types are the ones under
	// test. Neither handler is ever called.
	for _, reader := range []Reader{nil, NewPostgresReader(testenv.NewLogger(t), nil, nil)} {
		server, registrar := newServer(reader, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
		bindExternalTestPrincipal(server)
		registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})

		outputs := map[string]reflect.Type{}
		for _, descriptor := range registrar.Descriptors() {
			outputs[descriptor.Name] = descriptor.output
		}

		tools := listAdvertisedTools(t, server)
		require.NotEmpty(t, tools)
		checked := 0
		for _, tool := range tools {
			output, ok := outputs[tool.Name]
			require.True(t, ok, "%s is listed but has no descriptor", tool.Name)
			if output == reflect.TypeFor[any]() {
				require.Nil(t, tool.OutputSchema, "%s has an untyped result and advertises a schema anyway", tool.Name)
				continue
			}
			require.NotNil(t, tool.OutputSchema, "%s has a typed result and advertises no output schema", tool.Name)

			encodedSchema, err := json.Marshal(tool.OutputSchema)
			require.NoError(t, err)
			require.NotContains(t, string(encodedSchema), `"additionalProperties":false`,
				"%s advertises a closed object, so a client holding this schema rejects every result once a field is added", tool.Name)
			var schema jsonschema.Schema
			require.NoError(t, json.Unmarshal(encodedSchema, &schema))
			resolved, err := schema.Resolve(nil)
			require.NoError(t, err, tool.Name)

			if output.Kind() == reflect.Pointer {
				// The SDK substitutes the pointed-to zero value for a nil result,
				// so the schema describes the element, not a nullable pointer.
				output = output.Elem()
			}
			encoded, err := json.Marshal(sampleOf(t, output, tool.Name, map[reflect.Type]int{}).Interface())
			require.NoError(t, err, tool.Name)
			var decoded any
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			require.NoErrorf(t, resolved.Validate(decoded), "%s: a populated %s does not satisfy the advertised output schema\nresult: %s\nschema: %s", tool.Name, output, encoded, encodedSchema)
			checked++
		}
		require.Positive(t, checked, "no listed tool has a typed result, so nothing was validated")
	}
}

// The regression behind this test: #6766 added plugin_name and plugin_slug to
// MCPDistribution and stopped emitting state and publication_state for a
// membership the dashboard created. A client holding the schema from before
// that change rejected every find_mcp and get_mcp result that named a plugin,
// while a server with no plugin memberships kept working. Both membership
// shapes must satisfy the advertised schemas.
func TestFindAndGetMCPOutputSchemasAcceptPluginMemberships(t *testing.T) {
	t.Parallel()

	server, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	bindExternalTestPrincipal(server)
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})

	schemas := map[string]*jsonschema.Resolved{}
	for _, tool := range listAdvertisedTools(t, server) {
		if tool.Name != "find_mcp" && tool.Name != "get_mcp" {
			continue
		}
		encoded, err := json.Marshal(tool.OutputSchema)
		require.NoError(t, err)
		var schema jsonschema.Schema
		require.NoError(t, json.Unmarshal(encoded, &schema))
		schemas[tool.Name], err = schema.Resolve(nil)
		require.NoError(t, err)
	}
	require.Len(t, schemas, 2, "find_mcp and get_mcp are both listed with output schemas")

	item := MCP{
		ID:               "mcp",
		ProjectID:        "project",
		ProjectName:      "Project",
		ProjectSlug:      "project",
		Name:             "Reviewed",
		Slug:             "reviewed",
		Version:          "v1",
		Visibility:       "private",
		EffectiveEnabled: true,
		Model:            "platform_managed",
		BackendKind:      MCPBackendRemote,
		UpstreamURL:      "https://mcp.example.test/platform",
		Source:           MCPSource{Kind: "catalog", Provider: "registry", Reference: "reviewed/server"},
		Registration:     &MCPRegistration{ID: "registration", Status: "registered", ComponentsComplete: true},
		Readiness:        MCPReadiness{State: "ready", CheckedAt: "checked", ExpiresAt: "expires"},
		Distributions: []MCPDistribution{
			// A membership this flow created, seen by an org admin.
			{PluginID: "plugin-a", PluginName: "Plugin A", PluginSlug: "plugin-a", State: "attached", PublicationState: "published"},
			// A membership the dashboard created, seen by a member: no
			// lifecycle record and the plugin's name redacted.
			{PluginID: "plugin-b", PluginName: "", PluginSlug: "", State: "", PublicationState: ""},
		},
		Operations:    []string{"read"},
		DashboardPath: "/mcp/reviewed",
	}

	for name, output := range map[string]any{
		"find_mcp": FindMCPOutput{MCPs: []MCP{item}, NextCursor: "next"},
		"get_mcp":  item,
	} {
		encoded, err := json.Marshal(output)
		require.NoError(t, err)
		var decoded any
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.NoErrorf(t, schemas[name].Validate(decoded), "%s: %s", name, encoded)
	}
}

// A field added to a result type after a client fetched the schema must not
// fail that client's validation: an output schema promises what a result
// contains, and does not bound what it may contain.
func TestInferredOutputSchemaAcceptsFieldsAddedLater(t *testing.T) {
	t.Parallel()

	type before struct {
		Items []struct {
			PluginID string `json:"plugin_id"`
		} `json:"items"`
		Labels map[string]struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	type after struct {
		Items []struct {
			PluginID   string `json:"plugin_id"`
			PluginName string `json:"plugin_name"`
		} `json:"items"`
		Labels map[string]struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"labels"`
		Total int `json:"total"`
	}

	resolved, err := inferOutputSchema[before]("before").Resolve(nil)
	require.NoError(t, err)

	encoded, err := json.Marshal(sampleOf(t, reflect.TypeFor[after](), "after", map[reflect.Type]int{}).Interface())
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.NoError(t, resolved.Validate(decoded), "%s", encoded)

	// Opening objects must not open maps: a map's additionalProperties is its
	// value schema, and a value of the wrong shape is still rejected.
	require.ErrorContains(t, resolved.Validate(map[string]any{
		"items":  []any{},
		"labels": map[string]any{"key": "not an object"},
	}), "labels")
}

// listAdvertisedTools lists every tool through a real MCP session, following
// the server's pagination, so the assertion covers what a client actually sees.
func listAdvertisedTools(t *testing.T, server *mcp.Server) []*mcp.Tool {
	t.Helper()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "output-schema-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	var tools []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for {
		listed, err := session.ListTools(t.Context(), params)
		require.NoError(t, err)
		tools = append(tools, listed.Tools...)
		if listed.NextCursor == "" {
			return tools
		}
		params = &mcp.ListToolsParams{Cursor: listed.NextCursor}
	}
}

// sampleOf builds a value of typ with every field, element, and entry
// populated, so the JSON it marshals to exercises every property the schema
// names. Types whose wire form is not their Go shape come from sampleWireValues
// or, for the closed vocabularies in wireTypeSchemas, from the first enum value.
func sampleOf(t *testing.T, typ reflect.Type, path string, onStack map[reflect.Type]int) reflect.Value {
	t.Helper()

	if typ.Kind() == reflect.Pointer {
		pointer := reflect.New(typ.Elem())
		pointer.Elem().Set(sampleOf(t, typ.Elem(), path+"*", onStack))
		return pointer
	}
	if sample, ok := sampleWireValues[typ]; ok {
		return reflect.ValueOf(sample)
	}
	if override, ok := wireTypeSchemas[typ]; ok && len(override.Enum) > 0 {
		return reflect.ValueOf(override.Enum[0]).Convert(typ)
	}
	for _, marshaler := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()} {
		require.Falsef(t, typ.Implements(marshaler) || reflect.PointerTo(typ).Implements(marshaler),
			"%s: %s marshals itself, so its wire form cannot be derived from its Go shape; add it to sampleWireValues and wireTypeSchemas", path, typ)
	}

	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1.5)
	case reflect.String:
		value.SetString("sample")
	case reflect.Slice:
		value.Set(reflect.Append(reflect.MakeSlice(typ, 0, 1), sampleOf(t, typ.Elem(), path+"[]", onStack)))
	case reflect.Array:
		for i := range typ.Len() {
			value.Index(i).Set(sampleOf(t, typ.Elem(), path+"[]", onStack))
		}
	case reflect.Map:
		value.Set(reflect.MakeMap(typ))
		value.SetMapIndex(sampleOf(t, typ.Key(), path+"{key}", onStack), sampleOf(t, typ.Elem(), path+"{}", onStack))
	case reflect.Interface:
		require.Zerof(t, typ.NumMethod(), "%s: no sample for interface %s", path, typ)
		value.Set(reflect.ValueOf("sample"))
	case reflect.Struct:
		// A recursive type gets one populated level and a zero value below it;
		// the schema describes the recursion with $ref, which a zero value
		// still satisfies.
		if onStack[typ] > 0 {
			return value
		}
		onStack[typ]++
		defer func() { onStack[typ]-- }()
		for i := range typ.NumField() {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name == "-" {
				continue
			}
			value.Field(i).Set(sampleOf(t, field.Type, path+"."+field.Name, onStack))
		}
	default:
		require.Failf(t, "no sample", "%s: cannot build a sample of %s", path, typ)
	}
	return value
}
