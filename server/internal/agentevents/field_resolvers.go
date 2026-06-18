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

type FieldResolver[T any, V any] func(ev Event[T]) (V, bool, error)

func Resolve[T any, V any](field types.Field, resolve FieldResolver[T, V]) Resolver[T] {
	return Resolver[T]{
		Field: field,
		ResolveFunc: func(ev Event[T]) (any, bool, error) {
			return resolve(ev)
		},
	}
}

func GetValue[T any, V any](e Event[T], eventType types.EventType, field types.Field) (V, bool, error) {
	var zero V

	value, ok, err := e.resolve(resolveparams{eventType: eventType, field: field})
	if err != nil || !ok || value == nil {
		return zero, ok, err
	}

	typedValue, ok := value.(V)
	if !ok {
		return zero, false, fmt.Errorf("agentevents: field %s resolved to %T, want %T", field, value, zero)
	}
	return typedValue, true, nil
}

func GetField[T any, V any](name string) FieldResolver[T, V] {
	return func(ev Event[T]) (V, bool, error) {
		var zero V

		value, ok, err := rawStructField(ev.Raw(), name)
		if err != nil || !ok {
			return zero, false, err
		}

		value, ok = dereferenceFieldValue(value)
		if !ok {
			return zero, false, nil
		}

		typedValue, ok := value.Interface().(V)
		if !ok {
			return zero, false, fmt.Errorf("agentevents: field %s is %s, cannot cast to requested type", name, value.Type())
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
