package productmetrics

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"go.opentelemetry.io/otel/attribute"
)

// EncodedAttribute preserves an OTel value's type and exact JSON representation.
// Integer JSON is decoded only into int64, never into interface float64 values.
type EncodedAttribute struct {
	// Key is the unmodified attribute key.
	Key string `json:"key"`

	// Type distinguishes scalars and homogeneous arrays, including empty arrays.
	Type string `json:"type"`

	// Value is a canonical JSON scalar or array.
	Value json.RawMessage `json:"value"`
}

// CanonicalAttributes sorts keys without collapsing duplicates or namespaces.
func CanonicalAttributes(attrs []attribute.KeyValue) (string, error) {
	attrs = slices.Clone(attrs)
	slices.SortFunc(attrs, func(a, b attribute.KeyValue) int { return strings.Compare(string(a.Key), string(b.Key)) })
	encoded := make([]EncodedAttribute, 0, len(attrs))
	for i, a := range attrs {
		if a.Key == "" || (i > 0 && attrs[i-1].Key == a.Key) {
			return "", fmt.Errorf("empty or duplicate attribute key")
		}
		if a.Value.Type() == attribute.INVALID {
			return "", fmt.Errorf("invalid attribute type")
		}
		value := a.Value.AsInterface()
		// Normalize nil arrays to empty arrays while preserving their element type.
		switch a.Value.Type() {
		case attribute.STRING, attribute.BOOL, attribute.INT64, attribute.FLOAT64:
		case attribute.STRINGSLICE:
			value = append([]string{}, a.Value.AsStringSlice()...)
		case attribute.BOOLSLICE:
			value = append([]bool{}, a.Value.AsBoolSlice()...)
		case attribute.INT64SLICE:
			value = append([]int64{}, a.Value.AsInt64Slice()...)
		case attribute.FLOAT64SLICE:
			value = append([]float64{}, a.Value.AsFloat64Slice()...)
		case attribute.INVALID, attribute.BYTESLICE, attribute.SLICE:
			return "", fmt.Errorf("unsupported attribute type")
		}
		b, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("encode attribute: %w", err)
		}
		encoded = append(encoded, EncodedAttribute{Key: string(a.Key), Type: a.Value.Type().String(), Value: b})
	}
	b, err := json.Marshal(encoded)
	if err != nil {
		return "", fmt.Errorf("encode attributes: %w", err)
	}
	return string(b), nil
}

func attributesToProto(attrs []attribute.KeyValue) []*pmv1.Contribution_Attribute {
	result := make([]*pmv1.Contribution_Attribute, 0, len(attrs))
	for _, a := range attrs {
		v := &pmv1.Contribution_Value{}
		switch a.Value.Type() {
		case attribute.STRING:
			v.SetText(a.Value.AsString())
		case attribute.BOOL:
			v.SetBoolean(a.Value.AsBool())
		case attribute.INT64:
			v.SetInteger(a.Value.AsInt64())
		case attribute.FLOAT64:
			v.SetFloating(a.Value.AsFloat64())
		case attribute.STRINGSLICE:
			v.SetStrings(pmv1.Contribution_StringArray_builder{Values: a.Value.AsStringSlice()}.Build())
		case attribute.BOOLSLICE:
			v.SetBooleans(pmv1.Contribution_BoolArray_builder{Values: a.Value.AsBoolSlice()}.Build())
		case attribute.INT64SLICE:
			v.SetIntegers(pmv1.Contribution_IntArray_builder{Values: a.Value.AsInt64Slice()}.Build())
		case attribute.FLOAT64SLICE:
			v.SetDoubles(pmv1.Contribution_DoubleArray_builder{Values: a.Value.AsFloat64Slice()}.Build())
		case attribute.INVALID, attribute.BYTESLICE, attribute.SLICE:
			// Encode validates attributes before constructing their wire values.
		}
		result = append(result, pmv1.Contribution_Attribute_builder{Key: new(string(a.Key)), Value: v}.Build())
	}
	return result
}

func attributesFromProto(attrs []*pmv1.Contribution_Attribute) ([]attribute.KeyValue, error) {
	result := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		if a == nil || a.GetValue() == nil {
			return nil, fmt.Errorf("missing attribute value")
		}
		v := a.GetValue()
		var value attribute.Value
		switch {
		case v.HasText():
			value = attribute.StringValue(v.GetText())
		case v.HasBoolean():
			value = attribute.BoolValue(v.GetBoolean())
		case v.HasInteger():
			value = attribute.Int64Value(v.GetInteger())
		case v.HasFloating():
			value = attribute.Float64Value(v.GetFloating())
		case v.HasStrings():
			value = attribute.StringSliceValue(v.GetStrings().GetValues())
		case v.HasBooleans():
			value = attribute.BoolSliceValue(v.GetBooleans().GetValues())
		case v.HasIntegers():
			value = attribute.Int64SliceValue(v.GetIntegers().GetValues())
		case v.HasDoubles():
			value = attribute.Float64SliceValue(v.GetDoubles().GetValues())
		default:
			return nil, fmt.Errorf("unsupported attribute value")
		}
		result = append(result, attribute.KeyValue{Key: attribute.Key(a.GetKey()), Value: value})
	}
	return result, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
