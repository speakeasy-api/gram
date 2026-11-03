package authz

import (
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Operation is one typed authorization request: a registered permission on a
// concrete resource instance, or a named set of complete alternatives. Build
// operations with the typed resource constructors, such as
// Project(organizationID, projectID).Read(); services never assemble scopes,
// resource kinds or selectors themselves.
//
// The zero Operation is invalid and every client method rejects it.
type Operation struct {
	permission schema.PermissionRef
	instance   operationInstance

	// alternatives is set for a named alternatives operation, whose options
	// are complete single-permission operations in declaration order.
	alternatives bool
	options      []Operation
}

type operationInstance struct {
	resourceID    string
	dimensions    map[string]string
	strict        bool
	facts         map[string]Fact
	requiredFacts []string
}

// newOperation is the only way an Operation is built, so every operation
// names a registered permission; NewClient verifies each one the typed
// constructors use.
func newOperation(typeName, permission, resourceID string, dimensions map[string]string) Operation {
	return Operation{
		permission: schema.PermissionRef{Type: typeName, Permission: permission},
		instance: operationInstance{
			resourceID:    resourceID,
			dimensions:    maps.Clone(dimensions),
			strict:        false,
			facts:         nil,
			requiredFacts: nil,
		},
		alternatives: false,
		options:      nil,
	}
}

func newAlternatives(typeName, name string, options ...Operation) Operation {
	return Operation{
		permission:   schema.PermissionRef{Type: typeName, Permission: name},
		instance:     operationInstance{resourceID: "", dimensions: nil, strict: false, facts: nil, requiredFacts: nil},
		alternatives: true,
		options:      slices.Clone(options),
	}
}

// Permission names the registered permission or alternatives the operation
// asks for.
func (o Operation) Permission() schema.PermissionRef { return o.permission }

// ResourceID is the concrete resource the operation targets, or "" for an
// alternatives operation, whose options each name their own resource.
func (o Operation) ResourceID() string { return o.instance.resourceID }

// WithStrictMatch makes ordinary allows match strictly: a grant constraint
// the request does not carry no longer matches. It mirrors
// Check.WithStrictSelectorMatch for callers that already required it.
func (o Operation) WithStrictMatch() Operation {
	o.instance.strict = true
	o.instance.dimensions = maps.Clone(o.instance.dimensions)
	o.options = slices.Clone(o.options)
	for i := range o.options {
		o.options[i] = o.options[i].WithStrictMatch()
	}
	return o
}

// WithFacts attaches the verified facts a fact-reading permission needs.
// required lists the facts that must resolve before any branch is decided.
func (o Operation) WithFacts(facts map[string]Fact, required ...string) Operation {
	o.instance.facts = maps.Clone(facts)
	o.instance.requiredFacts = slices.Clone(required)
	return o
}

func (o Operation) String() string {
	if o.alternatives {
		return fmt.Sprintf("%s.%s", o.permission.Type, o.permission.Permission)
	}
	return fmt.Sprintf("%s.%s(%s)", o.permission.Type, o.permission.Permission, o.instance.resourceID)
}

func (o Operation) request() PermissionRequest {
	return PermissionRequest{
		Permission:      o.permission,
		ResourceID:      o.instance.resourceID,
		Dimensions:      o.instance.dimensions,
		StrictMatch:     o.instance.strict,
		RepresentedUser: "",
		Actor:           urn.Principal{Type: "", ID: ""},
		Facts:           o.instance.facts,
		RequiredFacts:   o.instance.requiredFacts,
	}
}

// OrganizationRef names one organization for typed operations.
type OrganizationRef struct {
	id string
}

// Organization names the organization with the given ID. The caller must
// have verified the organization belongs to the request's tenant.
func Organization(organizationID string) OrganizationRef {
	return OrganizationRef{id: organizationID}
}

// Read asks to read the organization.
func (o OrganizationRef) Read() Operation {
	return newOperation("organization", "read", o.id, nil)
}

// Administer asks to administer the organization.
func (o OrganizationRef) Administer() Operation {
	return newOperation("organization", "administer", o.id, nil)
}

// ProjectRef names one project in its organization for typed operations.
type ProjectRef struct {
	organizationID string
	id             string
}

// Project names a project. The caller must have verified that the project
// belongs to the organization.
func Project(organizationID, projectID string) ProjectRef {
	return ProjectRef{organizationID: organizationID, id: projectID}
}

// Read asks to read the project.
func (p ProjectRef) Read() Operation {
	return newOperation("project", "read", p.id, nil)
}

// Write asks to change the project.
func (p ProjectRef) Write() Operation {
	return newOperation("project", "write", p.id, nil)
}

// PublishPlugin asks to publish a plugin from the project: either project
// plugin publication or administration of its organization, with a block on
// either alternative vetoing both.
func (p ProjectRef) PublishPlugin() Operation {
	return newAlternatives("project", "plugin_publication",
		newOperation("project", "publish_plugin", p.id, nil),
		Organization(p.organizationID).Administer(),
	)
}

// typedOperations lists one instance of every operation the typed
// constructors build. NewClient rejects a registry that does not declare
// each of them, so a constructor can never name a missing permission at
// request time.
func typedOperations() []Operation {
	org := Organization("organization")
	project := Project("organization", "project")
	return []Operation{
		org.Read(), org.Administer(),
		project.Read(), project.Write(), project.PublishPlugin(),
	}
}
