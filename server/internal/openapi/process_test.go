package openapi

import (
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractJSONSchemaFromYaml_ExtractAndInlineLocalRef(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
openapi: 3.1.0
components:
  schemas:
    Foo:
      type: object
      properties:
        bar:
          $ref: '#/components/schemas/Bar'
    Bar:
      type: object
      properties:
        baz:
          type: string
`)

	document, err := libopenapi.NewDocumentWithConfiguration(yaml, &datamodel.DocumentConfiguration{
		AllowFileReferences:                 false,
		AllowRemoteReferences:               false,
		BundleInlineRefs:                    false,
		ExcludeExtensionRefs:                true,
		IgnorePolymorphicCircularReferences: true,
		IgnoreArrayCircularReferences:       true,
	})
	require.NoError(t, err)

	v3Model, errs := document.BuildV3Model()
	require.Empty(t, errs)

	schema, err := extractJSONSchemaFromYaml("Foo", v3Model.Model.Components.Schemas.GetOrZero("Foo"))
	require.NoError(t, err)

	assert.JSONEq(t, `{"type":"object","properties":{"bar":{"type":"object","properties":{"baz":{"type":"string"}}}}}`, string(schema))
}

func TestExtractJSONSchemaFromYaml_CircularRefError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml []byte
	}{
		{
			name: "Circular through a local ref",
			yaml: []byte(`
openapi: 3.1.0
components:
  schemas:
    Foo:
      type: object
      properties:
        bar:
          $ref: '#/components/schemas/Bar'
    Bar:
      type: object
      properties:
        baz:
          $ref: '#/components/schemas/Foo'
`),
		},
		{
			name: "Circular to self through array",
			yaml: []byte(`
openapi: 3.1.0
components:
  schemas:
    Foo:
      type: object
      properties:
        bar:
          type: array
          items:
            $ref: '#/components/schemas/Foo'
`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, err := libopenapi.NewDocumentWithConfiguration(test.yaml, &datamodel.DocumentConfiguration{
				AllowFileReferences:                 false,
				AllowRemoteReferences:               false,
				BundleInlineRefs:                    false,
				ExcludeExtensionRefs:                true,
				IgnorePolymorphicCircularReferences: true,
				IgnoreArrayCircularReferences:       true,
			})
			require.NoError(t, err)

			v3Model, errs := document.BuildV3Model()
			require.Empty(t, errs)

			_, err = extractJSONSchemaFromYaml("Foo", v3Model.Model.Components.Schemas.GetOrZero("Foo"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "circular reference")
		})
	}
}
