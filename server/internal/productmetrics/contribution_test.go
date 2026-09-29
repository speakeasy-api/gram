package productmetrics

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

func synthetic(instrument Instrument) Contribution {
	return Contribution{
		Tenant:     Tenant{OrganizationID: "synthetic-org", ProjectID: uuid.MustParse("00000000-0000-4000-8000-000000000001")},
		Definition: Definition{ScopeName: "gram.synthetic", ScopeVersion: "1", Name: "gram.synthetic.measurements", Description: "Synthetic measurements", Unit: "{measurement}", Instrument: instrument},
		ID:         "observation-1", EventTime: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, 9, 1, 12, 0, 1, 0, time.UTC),
		Value: Integer(1),
	}
}

func TestCanonicalIdentity(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	c.PointAttributes = []attribute.KeyValue{attribute.String("z", ""), attribute.Int64("a", math.MaxInt64)}
	a, err := Validate(c)
	require.NoError(t, err)
	c.PointAttributes[0], c.PointAttributes[1] = c.PointAttributes[1], c.PointAttributes[0]
	b, err := Validate(c)
	require.NoError(t, err)
	require.Equal(t, a, b)
	key, err := a.Key()
	require.NoError(t, err)
	require.Contains(t, key, "9223372036854775807")
	c.Definition.Description = "different metadata"
	b, err = Validate(c)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestSeriesDistinctions(t *testing.T) {
	t.Parallel()
	base := synthetic(Counter)
	a, err := Validate(base)
	require.NoError(t, err)
	cases := []struct {
		name   string
		change func(*Contribution)
	}{
		{"empty versus absent", func(c *Contribution) { c.PointAttributes = []attribute.KeyValue{attribute.String("key", "")} }},
		{"project", func(c *Contribution) { c.Tenant.ProjectID = uuid.MustParse("00000000-0000-4000-8000-000000000002") }},
		{"organization", func(c *Contribution) { c.Tenant.OrganizationID = "other-synthetic-org" }},
		{"numeric representation", func(c *Contribution) { c.Value = Float(1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := base
			tc.change(&c)
			b, err := Validate(c)
			require.NoError(t, err)
			require.NotEqual(t, a, b)
		})
	}
	keys := make(map[Series]bool)
	for _, value := range []attribute.Value{attribute.StringValue("1"), attribute.Int64Value(1), attribute.Float64Value(1), attribute.BoolValue(true), attribute.StringSliceValue([]string{}), attribute.Int64SliceValue([]int64{}), attribute.BoolSliceValue([]bool{}), attribute.Float64SliceValue([]float64{})} {
		for namespace := range 3 {
			c := base
			attrs := []attribute.KeyValue{{Key: "key", Value: value}}
			switch namespace {
			case 0:
				c.PointAttributes = attrs
			case 1:
				c.ResourceAttributes = attrs
			case 2:
				c.ScopeAttributes = attrs
			}
			s, err := Validate(c)
			require.NoError(t, err)
			require.False(t, keys[s])
			keys[s] = true
		}
	}
}

func TestWireRoundTrip(t *testing.T) {
	t.Parallel()
	for _, instrument := range []Instrument{Counter, Histogram} {
		t.Run(string(instrument), func(t *testing.T) {
			t.Parallel()
			c := synthetic(instrument)
			c.Value = Integer(math.MaxInt64)
			c.PointAttributes = []attribute.KeyValue{attribute.Int64("big", math.MaxInt64), attribute.StringSlice("empty", []string{}), attribute.Bool("enabled", true), attribute.Float64Slice("samples", []float64{1.5, 2.5})}
			m, err := Encode(c)
			require.NoError(t, err)
			wire, err := proto.Marshal(m)
			require.NoError(t, err)
			decoded := &pmv1.Contribution{}
			require.NoError(t, proto.Unmarshal(wire, decoded))
			got, s, err := Decode(decoded)
			require.NoError(t, err)
			want, err := Validate(c)
			require.NoError(t, err)
			require.Equal(t, c.Value, got.Value)
			require.Equal(t, want, s)
		})
	}
}

func TestInvalidContributions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*Contribution)
	}{
		{"missing tenant", func(c *Contribution) { c.Tenant.OrganizationID = "" }},
		{"missing project", func(c *Contribution) { c.Tenant.ProjectID = uuid.Nil }},
		{"missing value", func(c *Contribution) { c.Value = Number{} }},
		{"missing ID", func(c *Contribution) { c.ID = "" }},
		{"missing timestamp", func(c *Contribution) { c.EventTime = time.Time{} }},
		{"negative counter", func(c *Contribution) { c.Value = Integer(-1) }},
		{"nan", func(c *Contribution) { c.Value = Float(math.NaN()) }},
		{"infinity", func(c *Contribution) { c.Value = Float(math.Inf(1)) }},
		{"gauge", func(c *Contribution) { c.Definition.Instrument = "gauge" }},
		{"duplicate", func(c *Contribution) {
			c.PointAttributes = []attribute.KeyValue{attribute.String("a", "x"), attribute.String("a", "y")}
		}},
		{"reserved resource", func(c *Contribution) {
			c.ResourceAttributes = []attribute.KeyValue{attribute.String(ProjectResourceKey, c.Tenant.ProjectID.String())}
		}},
		{"nan array", func(c *Contribution) {
			c.PointAttributes = []attribute.KeyValue{attribute.Float64Slice("a", []float64{math.NaN()})}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := synthetic(Counter)
			tc.change(&c)
			_, err := Encode(c)
			require.Error(t, err)
		})
	}
}

func TestRejectWireSemantics(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"temporality", "value", "timestamp", "definition"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			m, err := Encode(synthetic(Counter))
			require.NoError(t, err)
			switch field {
			case "temporality":
				m.GetDefinition().SetTemporality("cumulative")
			case "value":
				m.ClearValue()
			case "timestamp":
				m.ClearEventTimeUnixNano()
			case "definition":
				m.ClearDefinition()
			}
			_, _, err = Decode(m)
			require.Error(t, err)
		})
	}
}

func TestRegistryConflicts(t *testing.T) {
	t.Parallel()
	d := synthetic(Counter).Definition
	r, err := NewRegistry(d)
	require.NoError(t, err)
	d.Description = "new description"
	require.NoError(t, r.Register(d))
	d.Unit = "s"
	require.Error(t, r.Register(d))
	d.ScopeVersion = "2"
	require.Error(t, r.Register(d))
	d.Unit = "{measurement}"
	d.Instrument = Histogram
	require.Error(t, r.Register(d))
}

type capturePublisher struct{ messages []*pmv1.Contribution }

func (p *capturePublisher) Publish(_ context.Context, m *pmv1.Contribution, _ ...gcp.PublishOption) gcp.PublishResult {
	p.messages = append(p.messages, m)
	return gcp.NewSuccessPublishResult()
}
func (p *capturePublisher) Stop(context.Context) error { return nil }

func TestPublisherRequiresRegistration(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	r, err := NewRegistry()
	require.NoError(t, err)
	capture := &capturePublisher{}
	p := NewPublisher(r, capture)
	_, err = p.Publish(t.Context(), c).Get(t.Context())
	require.Error(t, err)
	require.Empty(t, capture.messages)
	require.NoError(t, r.Register(c.Definition))
	_, err = p.Publish(t.Context(), c).Get(t.Context())
	require.NoError(t, err)
	require.Len(t, capture.messages, 1)
	c.Value = Integer(-1)
	_, err = p.Publish(t.Context(), c).Get(t.Context())
	require.Error(t, err)
	require.Len(t, capture.messages, 1)
}
