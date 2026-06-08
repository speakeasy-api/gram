package agentevents

import (
	"fmt"
	"reflect"
)

func StringField[T any](name string) FieldResolver[T] {
	return func(ev Event[T]) (any, bool, error) {
		value, ok, err := rawStructField(ev.Raw, name)
		if err != nil || !ok {
			return nil, false, err
		}
		value, ok = dereferenceFieldValue(value)
		if !ok {
			return nil, false, nil
		}
		if value.Kind() != reflect.String {
			return nil, false, fmt.Errorf("agentevents: field %s resolved to %s, want string", name, value.Type())
		}
		str := value.String()
		if str == "" {
			return nil, false, nil
		}
		return str, true, nil
	}
}

func IntField[T any](name string) FieldResolver[T] {
	return func(ev Event[T]) (any, bool, error) {
		value, ok, err := rawStructField(ev.Raw, name)
		if err != nil || !ok {
			return nil, false, err
		}
		value, ok = dereferenceFieldValue(value)
		if !ok {
			return nil, false, nil
		}
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return int(value.Int()), true, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return int(value.Uint()), true, nil
		default:
			return nil, false, fmt.Errorf("agentevents: field %s resolved to %s, want int", name, value.Type())
		}
	}
}

func AnyField[T any](name string) FieldResolver[T] {
	return func(ev Event[T]) (any, bool, error) {
		value, ok, err := rawStructField(ev.Raw, name)
		if err != nil || !ok {
			return nil, false, err
		}
		value, ok = dereferenceFieldValue(value)
		if !ok {
			return nil, false, nil
		}
		return value.Interface(), true, nil
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
