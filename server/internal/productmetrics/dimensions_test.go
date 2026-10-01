package productmetrics

import (
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"testing"
)

func TestDimensionContract(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	c.ResourceAttributes = nil
	c.ScopeAttributes = nil
	c.PointAttributes = []attribute.KeyValue{attribute.String("model", "small")}
	r, err := NewRegistry(c.Definition)
	require.NoError(t, err)
	require.Error(t, r.RegisterDimensions(c.Definition, AttributeRule{Namespace: "point", Key: "payload", Type: attribute.BYTESLICE}))
	require.NoError(t, r.RegisterDimensions(c.Definition, AttributeRule{Namespace: "point", Key: "model", Type: attribute.STRING, Values: []attribute.Value{attribute.StringValue("small")}}))
	require.NoError(t, r.ValidateDimensions(c))
	c.PointAttributes = []attribute.KeyValue{attribute.String("model", "unbounded")}
	require.Error(t, r.ValidateDimensions(c))
	c.PointAttributes = []attribute.KeyValue{attribute.Int64("model", 1)}
	require.Error(t, r.ValidateDimensions(c))
	c.PointAttributes = []attribute.KeyValue{attribute.String("request_id", "unique")}
	require.Error(t, r.ValidateDimensions(c))
	require.NoError(t, r.RegisterDimensions(c.Definition))
	c.PointAttributes = nil
	require.NoError(t, r.ValidateDimensions(c))
}
