package serialization

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

func parseFormParams(paramName string, objType reflect.Type, objValue reflect.Value, delimiter string, explode bool) url.Values {

	formValues := url.Values{}

	if isNil(objType, objValue) {
		return formValues
	}

	if objType.Kind() == reflect.Pointer {
		objType = objType.Elem()
		objValue = objValue.Elem()
	}

	switch objType.Kind() {
	case reflect.Struct:
		var items []string

		for i := range objType.NumField() {
			fieldType := objType.Field(i)
			valType := objValue.Field(i)

			if isNil(fieldType.Type, valType) {
				continue
			}

			if valType.Kind() == reflect.Pointer {
				valType = valType.Elem()
			}

			if explode {
				formValues.Add(paramName, valToString(valType.Interface()))
			} else {
				items = append(items, fmt.Sprintf("%s%s%s", paramName, delimiter, valToString(valType.Interface())))
			}
		}

		if len(items) > 0 {
			formValues.Add(paramName, strings.Join(items, delimiter))
		}
	case reflect.Map:
		items := []string{}

		iter := objValue.MapRange()
		for iter.Next() {
			if explode {
				formValues.Add(iter.Key().String(), valToString(iter.Value().Interface()))
			} else {
				items = append(items, fmt.Sprintf("%s%s%s", iter.Key().String(), delimiter, valToString(iter.Value().Interface())))
			}
		}

		if len(items) > 0 {
			formValues.Add(paramName, strings.Join(items, delimiter))
		}
	case reflect.Slice, reflect.Array:
		values := parseDelimitedArray(explode, objValue, delimiter)
		for _, v := range values {
			formValues.Add(paramName, v)
		}
	default:
		formValues.Add(paramName, valToString(objValue.Interface()))
	}

	return formValues
}
