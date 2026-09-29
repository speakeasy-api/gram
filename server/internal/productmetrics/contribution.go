package productmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// OrganizationResourceKey is Gram's custom OTel tenant ownership attribute.
	OrganizationResourceKey = "gram.organization.id"
	// ProjectResourceKey is Gram's custom OTel project ownership attribute.
	ProjectResourceKey = "gram.project.id"
)

// Tenant is mandatory typed ownership, never inferred from producer attributes.
type Tenant struct {
	// OrganizationID identifies the owning organization.
	OrganizationID string `json:"organization_id"`

	// ProjectID identifies the owning project.
	ProjectID uuid.UUID `json:"project_id"`
}

// Number preserves integer precision; its zero value is intentionally invalid.
type Number struct {
	integer  int64
	floating float64
	kind     string
}

// Integer constructs an exact signed integer contribution.
func Integer(value int64) Number { return Number{integer: value, floating: 0, kind: "integer"} }

// Float constructs a floating-point contribution, validated before publication.
func Float(value float64) Number { return Number{integer: 0, floating: value, kind: "floating"} }

// Contribution is one producer-owned observation. IDs are diagnostic delivery
// identities, never metric dimensions. Retry an observation with the same ID.
type Contribution struct {
	// Tenant is the mandatory organization/project ownership.
	Tenant Tenant

	// Definition is registered by the producer at startup.
	Definition Definition

	// ID is unique per observation within tenant and instrumentation scope.
	ID string

	// EventTime is the source observation time.
	EventTime time.Time

	// ObservedAt is the time the producer observed the contribution.
	ObservedAt time.Time

	// ResourceAttributes describe the measured resource.
	ResourceAttributes []attribute.KeyValue

	// ScopeAttributes describe the instrumentation scope.
	ScopeAttributes []attribute.KeyValue

	// PointAttributes are producer-selected metric dimensions.
	PointAttributes []attribute.KeyValue

	// Value is one counter increment or histogram observation.
	Value Number
}

// Series preserves full canonical identity rather than a fingerprint. Number
// representation is included so integer aggregation never coerces to float64.
type Series struct {
	// Tenant is typed ownership, also present in Resource.
	Tenant Tenant `json:"tenant"`

	// Descriptor excludes description metadata.
	Descriptor Definition `json:"descriptor"`

	// Resource is canonical resource attributes including typed ownership.
	Resource string `json:"resource"`

	// Scope is canonical instrumentation scope attributes.
	Scope string `json:"scope"`

	// Point is canonical point attributes.
	Point string `json:"point"`

	// NumberKind preserves the integer or floating representation.
	NumberKind string `json:"number_kind"`
}

// Validate checks the contract and returns a lossless, comparable series key.
func Validate(c Contribution) (Series, error) {
	var zero Series
	if c.Tenant.OrganizationID == "" || c.Tenant.ProjectID == uuid.Nil || c.ID == "" {
		return zero, fmt.Errorf("tenant, project and contribution ID are required")
	}
	if c.EventTime.IsZero() || c.ObservedAt.IsZero() || !c.EventTime.Equal(time.Unix(0, c.EventTime.UnixNano())) || !c.ObservedAt.Equal(time.Unix(0, c.ObservedAt.UnixNano())) {
		return zero, fmt.Errorf("timestamps must be present and representable as Unix nanoseconds")
	}
	if _, err := NewRegistry(c.Definition); err != nil {
		return zero, err
	}
	if c.Value.kind != "integer" && c.Value.kind != "floating" {
		return zero, fmt.Errorf("numeric value is required")
	}
	if !finite(c.Value.floating) || (c.Definition.Instrument == Counter && (c.Value.integer < 0 || c.Value.floating < 0)) {
		return zero, fmt.Errorf("value must be finite and counter increments nonnegative")
	}
	resource := append([]attribute.KeyValue{}, c.ResourceAttributes...)
	// Ownership is emitted exactly once; even matching explicit overrides are rejected.
	for _, a := range resource {
		if string(a.Key) == OrganizationResourceKey || string(a.Key) == ProjectResourceKey {
			return zero, fmt.Errorf("reserved ownership resource attribute")
		}
	}
	resource = append(resource, attribute.String(OrganizationResourceKey, c.Tenant.OrganizationID), attribute.String(ProjectResourceKey, c.Tenant.ProjectID.String()))
	r, err := CanonicalAttributes(resource)
	if err != nil {
		return zero, err
	}
	s, err := CanonicalAttributes(c.ScopeAttributes)
	if err != nil {
		return zero, err
	}
	p, err := CanonicalAttributes(c.PointAttributes)
	if err != nil {
		return zero, err
	}
	d := c.Definition
	d.Description = ""
	return Series{Tenant: c.Tenant, Descriptor: d, Resource: r, Scope: s, Point: p, NumberKind: c.Value.kind}, nil
}

// Key returns the full series identity, not a hash susceptible to collisions.
func (s Series) Key() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("encode series: %w", err)
	}
	return string(b), nil
}

// Publisher combines explicit definition registration with the shared broker.
type Publisher struct {
	registry  *Registry
	publisher gcp.Publisher[*pmv1.Contribution]
}

// NewPublisher borrows the broker publisher; its owner controls shutdown.
func NewPublisher(registry *Registry, publisher gcp.Publisher[*pmv1.Contribution]) *Publisher {
	return &Publisher{registry: registry, publisher: publisher}
}

// Publish validates before handing the immutable wire copy to the broker. The
// caller must await Get(ctx) and owns any business transaction/outbox boundary.
func (p *Publisher) Publish(ctx context.Context, c Contribution) gcp.PublishResult {
	if p.registry == nil || !p.registry.contains(c.Definition) {
		return gcp.NewErrPublishResult(fmt.Errorf("metric definition is not registered"))
	}
	m, err := Encode(c)
	if err != nil {
		return gcp.NewErrPublishResult(err)
	}
	return p.publisher.Publish(ctx, m)
}

// Encode validates a contribution and builds its wire representation.
func Encode(c Contribution) (*pmv1.Contribution, error) {
	if _, err := Validate(c); err != nil {
		return nil, err
	}
	v := &pmv1.Contribution_Number{}
	if c.Value.kind == "integer" {
		v.SetInteger(c.Value.integer)
	} else {
		v.SetFloating(c.Value.floating)
	}
	d := c.Definition
	return pmv1.Contribution_builder{
		OrganizationId: new(c.Tenant.OrganizationID), ProjectId: new(c.Tenant.ProjectID.String()),
		ContributionId: new(c.ID), EventTimeUnixNano: new(c.EventTime.UnixNano()), ObservedAtUnixNano: new(c.ObservedAt.UnixNano()),
		Definition:         pmv1.Contribution_Definition_builder{ScopeName: new(d.ScopeName), ScopeVersion: new(d.ScopeVersion), Name: new(d.Name), Description: new(d.Description), Unit: new(d.Unit), Instrument: new(string(d.Instrument)), Temporality: new("delta")}.Build(),
		ResourceAttributes: attributesToProto(c.ResourceAttributes), ScopeAttributes: attributesToProto(c.ScopeAttributes), PointAttributes: attributesToProto(c.PointAttributes), Value: v,
	}.Build(), nil
}

// Decode rejects unsupported instrument/temporality and missing fields rather
// than reinterpreting cumulative counters or preaggregated measurements.
func Decode(m *pmv1.Contribution) (Contribution, Series, error) {
	var c Contribution
	var s Series
	if m == nil || m.GetDefinition() == nil || !m.HasEventTimeUnixNano() || !m.HasObservedAtUnixNano() || m.GetValue() == nil {
		return c, s, fmt.Errorf("missing contribution fields")
	}
	d := m.GetDefinition()
	if d.GetTemporality() != "delta" {
		return c, s, fmt.Errorf("unsupported temporality")
	}
	project, err := uuid.Parse(m.GetProjectId())
	if err != nil {
		return c, s, fmt.Errorf("invalid project: %w", err)
	}
	c.Tenant = Tenant{OrganizationID: m.GetOrganizationId(), ProjectID: project}
	c.Definition = Definition{ScopeName: d.GetScopeName(), ScopeVersion: d.GetScopeVersion(), Name: d.GetName(), Description: d.GetDescription(), Unit: d.GetUnit(), Instrument: Instrument(d.GetInstrument())}
	c.ID = m.GetContributionId()
	c.EventTime = time.Unix(0, m.GetEventTimeUnixNano()).UTC()
	c.ObservedAt = time.Unix(0, m.GetObservedAtUnixNano()).UTC()
	if m.GetValue().HasInteger() {
		c.Value = Integer(m.GetValue().GetInteger())
	} else if m.GetValue().HasFloating() {
		c.Value = Float(m.GetValue().GetFloating())
	}
	if c.ResourceAttributes, err = attributesFromProto(m.GetResourceAttributes()); err != nil {
		return c, s, err
	}
	if c.ScopeAttributes, err = attributesFromProto(m.GetScopeAttributes()); err != nil {
		return c, s, err
	}
	if c.PointAttributes, err = attributesFromProto(m.GetPointAttributes()); err != nil {
		return c, s, err
	}
	s, err = Validate(c)
	return c, s, err
}
