package openapi

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"gopkg.in/yaml.v3"
)

type UpgradeOpenAPI30To31Result struct {
	Document libopenapi.Document
	Model    *libopenapi.DocumentModel[v3.Document]
	Issues   []error
}

// UpgradeOpenAPI30To31 upgrades an OpenAPI 3.0 document to OpenAPI 3.1.
// If the upgrade fails due to an error then the original document and model are
// returned along with the errors.
func UpgradeOpenAPI30To31(doc libopenapi.Document, docModel *libopenapi.DocumentModel[v3.Document]) (*UpgradeOpenAPI30To31Result, error) {
	if !strings.HasPrefix(docModel.Model.Version, "3.0") {
		return &UpgradeOpenAPI30To31Result{
			Document: doc,
			Model:    docModel,
			Issues:   []error{},
		}, nil
	}

	docModel.Model.Version = "3.1.0"

	var issues []error

	if docModel.Model.Components != nil {
		if docModel.Model.Components.Schemas != nil {
			for _, schema := range docModel.Model.Components.Schemas.FromOldest() {
				if err := upgradeSchema(schema); err != nil {
					issues = append(issues, fmt.Errorf("error upgrading component schema: %w", err))
				}
			}
		}

		if docModel.Model.Components.Parameters != nil {
			for _, param := range docModel.Model.Components.Parameters.FromOldest() {
				if err := upgradeParameter(param); err != nil {
					issues = append(issues, fmt.Errorf("error upgrading component parameter: %w", err))
				}
			}
		}

		if docModel.Model.Components.RequestBodies != nil {
			for _, rb := range docModel.Model.Components.RequestBodies.FromOldest() {
				if rb.Content != nil {
					for _, mediaType := range rb.Content.FromOldest() {
						if mediaType.Schema != nil {
							if err := upgradeSchema(mediaType.Schema); err != nil {
								issues = append(issues, fmt.Errorf("error upgrading component request body schema: %w", err))
							}
						}
					}
				}
			}
		}
	}

	issues = append(issues, upgradePathItems(docModel)...)

	_, upgradedDoc, upgradedModel, rrErrs := doc.RenderAndReload()
	if len(rrErrs) > 0 {
		return nil, fmt.Errorf("error rebuilding upgraded openapi document: %w", errors.Join(rrErrs...))
	}

	return &UpgradeOpenAPI30To31Result{
		Document: upgradedDoc,
		Model:    upgradedModel,
		Issues:   issues,
	}, nil
}

func upgradePathItems(docModel *libopenapi.DocumentModel[v3.Document]) []error {
	var errs []error

	if docModel.Model.Paths == nil || docModel.Model.Paths.PathItems == nil {
		return errs
	}

	for path, pitem := range docModel.Model.Paths.PathItems.FromOldest() {
		for _, sharedParam := range pitem.Parameters {
			if err := upgradeParameter(sharedParam); err != nil {
				errs = append(errs, fmt.Errorf("%s: error upgrading shared parameter: %w", path, err))
			}
		}

		if op, err := pitem.Get, upgradeOperation(pitem.Get); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "GET", locForOperation(op), path, err))
		}
		if op, err := pitem.Put, upgradeOperation(pitem.Put); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "PUT", locForOperation(op), path, err))
		}
		if op, err := pitem.Post, upgradeOperation(pitem.Post); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "POST", locForOperation(op), path, err))
		}
		if op, err := pitem.Delete, upgradeOperation(pitem.Delete); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "DELETE", locForOperation(op), path, err))
		}
		if op, err := pitem.Options, upgradeOperation(pitem.Options); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "OPTIONS", locForOperation(op), path, err))
		}
		if op, err := pitem.Head, upgradeOperation(pitem.Head); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "HEAD", locForOperation(op), path, err))
		}
		if op, err := pitem.Patch, upgradeOperation(pitem.Patch); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "PATCH", locForOperation(op), path, err))
		}
		if op, err := pitem.Trace, upgradeOperation(pitem.Trace); err != nil {
			errs = append(errs, fmt.Errorf("%s %s %s: error upgrading operation: %w", "TRACE", locForOperation(op), path, err))
		}
	}

	return errs
}

func upgradeOperation(op *v3.Operation) error {
	if op == nil {
		return nil
	}

	for _, param := range op.Parameters {
		if err := upgradeParameter(param); err != nil {
			return fmt.Errorf("upgrade operation parameter: %w", err)
		}
	}

	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for _, mediaType := range op.RequestBody.Content.FromOldest() {
			if mediaType.Schema != nil {
				if err := upgradeSchema(mediaType.Schema); err != nil {
					return fmt.Errorf("upgrade request body schema: %w", err)
				}
			}
		}
	}

	return nil
}

func upgradeParameter(param *v3.Parameter) error {
	if param == nil || param.Schema == nil {
		return nil
	}

	return upgradeSchema(param.Schema)
}

func upgradeSchema(schemaProxy *base.SchemaProxy) error {
	if schemaProxy == nil || schemaProxy.IsReference() {
		return nil
	}

	loc := "[:]"
	low := schemaProxy.GoLow()
	if low != nil {
		keyNode := low.GetKeyNode()
		if keyNode != nil {
			loc = fmt.Sprintf("[%d:%d]", keyNode.Line, keyNode.Column)
		}
	}

	schema, err := schemaProxy.BuildSchema()
	if err != nil {
		return fmt.Errorf("build schema %s: %w", loc, err)
	}

	updgradeExample(schema)
	upgradeExclusiveMinMax(schema)
	if err := upgradeNullableSchema(schema); err != nil {
		return fmt.Errorf("error upgrading nullable schema %s: %w", loc, err)
	}

	// Recursively upgrade nested schemas
	for _, subSchema := range schema.AllOf {
		if err := upgradeSchema(subSchema); err != nil {
			return fmt.Errorf("error upgrading subschema (allOf): %w", err)
		}
	}
	for _, subSchema := range schema.AnyOf {
		if err := upgradeSchema(subSchema); err != nil {
			return fmt.Errorf("error upgrading subschema (anyOf): %w", err)
		}
	}
	for _, subSchema := range schema.OneOf {
		if err := upgradeSchema(subSchema); err != nil {
			return fmt.Errorf("error upgrading subschema (oneOf): %w", err)
		}
	}

	if schema.Items != nil && schema.Items.IsA() {
		if err := upgradeSchema(schema.Items.A); err != nil {
			return fmt.Errorf("error upgrading subschema (items): %w", err)
		}
	}

	if schema.Properties != nil {
		for key, propSchema := range schema.Properties.FromOldest() {
			_ = key
			if err := upgradeSchema(propSchema); err != nil {
				return fmt.Errorf("error upgrading subschema (property): %w", err)
			}
		}
	}

	if schema.AdditionalProperties != nil && schema.AdditionalProperties.IsA() {
		if err := upgradeSchema(schema.AdditionalProperties.A); err != nil {
			return fmt.Errorf("error upgrading subschema (additional property): %w", err)
		}
	}

	return nil
}

func updgradeExample(schema *base.Schema) {
	if schema == nil || schema.Example == nil {
		return
	}

	if schema.Examples == nil {
		schema.Examples = []*yaml.Node{}
	}

	schema.Examples = append(schema.Examples, schema.Example)
	schema.Example = nil
}

func upgradeExclusiveMinMax(schema *base.Schema) {
	if schema.ExclusiveMaximum != nil && schema.ExclusiveMaximum.IsA() {
		if schema.Maximum == nil {
			schema.ExclusiveMaximum = nil
		} else {
			schema.ExclusiveMaximum = &base.DynamicValue[bool, float64]{
				N: 1,
				B: *schema.Maximum,
			}
			schema.Maximum = nil
		}
	}

	if schema.ExclusiveMinimum != nil && schema.ExclusiveMinimum.IsA() {
		if schema.Minimum == nil {
			schema.ExclusiveMinimum = nil
		} else {
			schema.ExclusiveMinimum = &base.DynamicValue[bool, float64]{
				N: 1,
				B: *schema.Minimum,
			}
			schema.Minimum = nil
		}
	}
}

func upgradeNullableSchema(schema *base.Schema) error {
	if schema == nil {
		return nil
	}

	if schema.Nullable == nil || !*schema.Nullable {
		schema.Nullable = nil // clear it out if it was set to false
		return nil
	}

	schema.Nullable = nil

	switch {
	case len(schema.Type) > 0:
		if !slices.Contains(schema.Type, "null") {
			schema.Type = append(schema.Type, "null")
		}
	case len(schema.AnyOf) > 0:
		nullSchema := createNullSchema()
		schema.AnyOf = append(schema.AnyOf, nullSchema)
	case len(schema.OneOf) > 0:
		nullSchema := createNullSchema()
		schema.OneOf = append(schema.OneOf, nullSchema)
	default:
		nullSchema := createNullSchema()
		clone := *schema
		newSchema := &base.Schema{}
		newSchema.OneOf = []*base.SchemaProxy{nullSchema, base.CreateSchemaProxy(&clone)}
		*schema = *newSchema
	}

	return nil
}

func locForOperation(op *v3.Operation) string {
	if op == nil {
		return "[:]"
	}

	low := op.GoLow()
	if low == nil {
		return "[:]"
	}

	keyNode := low.GetKeyNode()
	if keyNode == nil {
		return "[:]"
	}

	return fmt.Sprintf("[%d:%d]", keyNode.Line, keyNode.Column)
}

func createNullSchema() *base.SchemaProxy {
	nullSchema := &base.Schema{}
	nullSchema.Type = []string{"null"}
	return base.CreateSchemaProxy(nullSchema)
}
