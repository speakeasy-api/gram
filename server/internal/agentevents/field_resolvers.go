package agentevents

import (
	"fmt"
	"reflect"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

type Resolver[T any] struct {
	Field       types.Field
	ResolveFunc FieldResolver[T, any]
}

func Resolve[T any, V any](field types.Field, resolve FieldResolver[T, V]) Resolver[T] {
	return Resolver[T]{
		Field: field,
		ResolveFunc: func(ev Event[T]) (any, bool, error) {
			value, ok, err := resolve(ev)
			return value, ok, err
		},
	}
}

type FieldResolver[StructType any, FieldType any] func(ev Event[StructType]) (FieldType, bool, error)

func GetValue[T any, V any](e Event[T], field types.Field) (V, bool, error) {
	var zero V

	value, ok, err := e.resolve(field)
	if err != nil || !ok || value == nil {
		return zero, ok, err
	}

	typedValue, ok := value.(V)
	if !ok {
		return zero, false, fmt.Errorf("agentevents: field %s resolved to %T, want %T", field, value, zero)
	}
	return typedValue, true, nil
}

func GetField[StructType any, FieldType any](name string) FieldResolver[StructType, FieldType] {
	return func(ev Event[StructType]) (FieldType, bool, error) {
		// 1. Zero value helper for empty returns
		var zero FieldType

		value, ok, err := rawStructField(ev.Raw, name)
		if err != nil || !ok {
			return zero, false, err
		}

		value, ok = dereferenceFieldValue(value)
		if !ok {
			return zero, false, nil
		}

		// 2. Extract the raw interface value from reflect
		rawInterface := value.Interface()

		// 3. Perform a Runtime Type Assertion to bridge into Compile-Time Generics
		typedValue, ok := rawInterface.(FieldType)
		if !ok {
			return zero, false, fmt.Errorf("field %s is type %s, cannot cast to requested type", name, value.Type())
		}

		return typedValue, true, nil
	}
}

func rawStructField[T any](raw T, name string) (reflect.Value, bool, error) {
	if name == "" {
		return reflect.Value{}, false, fmt.Errorf("agentevents: empty raw field name")
	}

	value := reflect.ValueOf(raw)
	value, ok := dereferenceFieldValue(value)

	if !ok {
		return reflect.Value{}, false, nil
	}

	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false, fmt.Errorf("agentevents: raw payload resolved to %s, want struct", value.Type())
	}

	field := value.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, false, fmt.Errorf("agentevents: raw payload has no field %s", name)
	}

	if !field.CanInterface() {
		return reflect.Value{}, false, fmt.Errorf("agentevents: raw payload field %s is not exported", name)
	}

	return field, true, nil
}

func dereferenceFieldValue(value reflect.Value) (reflect.Value, bool) {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, value.IsValid()
}
