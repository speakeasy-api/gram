package serialization

import (
	"net/url"
	"reflect"
	"time"
)

func parseDeepObjectParams(paramName string, objType reflect.Type, objValue reflect.Value) url.Values {
	values := url.Values{}

	if isNil(objType, objValue) {
		return values
	}

	if objValue.Kind() == reflect.Pointer {
		objValue = objValue.Elem()
	}

	switch objValue.Kind() {
	case reflect.Map:
		populateDeepObjectParamsMap(values, paramName, objValue)
	case reflect.Struct:
		populateDeepObjectParamsStruct(values, paramName, objValue)
	default:
		values.Add(paramName, valToString(objValue.Interface()))
	}

	return values
}

func populateDeepObjectParamsStruct(qsValues url.Values, priorScope string, structValue reflect.Value) {
	if structValue.Kind() != reflect.Struct {
		return
	}

	structType := structValue.Type()

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldValue := structValue.Field(i)

		if isNil(field.Type, fieldValue) {
			continue
		}

		if fieldValue.Kind() == reflect.Pointer {
			fieldValue = fieldValue.Elem()
		}

		scope := priorScope + "[" + field.Name + "]"

		switch fieldValue.Kind() {
		case reflect.Array, reflect.Slice:
			populateDeepObjectParamsArray(qsValues, scope, fieldValue)
		case reflect.Map:
			populateDeepObjectParamsMap(qsValues, scope, fieldValue)
		case reflect.Struct:
			if fieldValue.Type() == reflect.TypeFor[time.Time]() {
				qsValues.Add(scope, valToString(fieldValue.Interface()))

				continue
			}

			populateDeepObjectParamsStruct(qsValues, scope, fieldValue)
		default:
			qsValues.Add(scope, valToString(fieldValue.Interface()))
		}
	}
}

func populateDeepObjectParamsMap(qsValues url.Values, priorScope string, mapValue reflect.Value) {
	if mapValue.Kind() != reflect.Map {
		return
	}

	iter := mapValue.MapRange()

	for iter.Next() {
		scope := priorScope + "[" + iter.Key().String() + "]"
		iterValue := iter.Value()

		switch iterValue.Kind() {
		case reflect.Array, reflect.Slice:
			populateDeepObjectParamsArray(qsValues, scope, iterValue)
		case reflect.Map:
			populateDeepObjectParamsMap(qsValues, scope, iterValue)
		default:
			qsValues.Add(scope, valToString(iterValue.Interface()))
		}
	}
}

func populateDeepObjectParamsArray(qsValues url.Values, priorScope string, value reflect.Value) {
	if value.Kind() != reflect.Array && value.Kind() != reflect.Slice {
		return
	}

	for i := 0; i < value.Len(); i++ {
		qsValues.Add(priorScope, valToString(value.Index(i).Interface()))
	}
}
