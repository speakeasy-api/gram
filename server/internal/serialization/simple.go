package serialization

import (
	"fmt"
	"reflect"
	"strings"
)

// parseSimpleParams parses path parameters and headers using simple style encoding.
func parseSimpleParams(parentName string, objType reflect.Type, objValue reflect.Value, explode bool) map[string]string {
	pathParams := make(map[string]string)

	if isNil(objType, objValue) {
		return nil
	}

	if objType.Kind() == reflect.Pointer {
		objType = objType.Elem()
		objValue = objValue.Elem()
	}

	switch objType.Kind() {
	case reflect.Array, reflect.Slice:
		if objValue.Len() == 0 {
			return nil
		}
		var ppVals []string
		for i := range objValue.Len() {
			ppVals = append(ppVals, valToString(objValue.Index(i).Interface()))
		}
		pathParams[parentName] = strings.Join(ppVals, ",")
	case reflect.Map:
		if objValue.Len() == 0 {
			return nil
		}
		var ppVals []string
		objMap := objValue.MapRange()
		for objMap.Next() {
			if explode {
				ppVals = append(ppVals, fmt.Sprintf("%s=%s", objMap.Key().String(), valToString(objMap.Value().Interface())))
			} else {
				ppVals = append(ppVals, fmt.Sprintf("%s,%s", objMap.Key().String(), valToString(objMap.Value().Interface())))
			}
		}
		pathParams[parentName] = strings.Join(ppVals, ",")
	case reflect.Struct:
		var ppVals []string
		for i := range objType.NumField() {
			fieldType := objType.Field(i)
			valType := objValue.Field(i)

			if isNil(fieldType.Type, valType) {
				continue
			}

			if fieldType.Type.Kind() == reflect.Pointer {
				valType = valType.Elem()
			}

			fieldName := fieldType.Name
			if explode {
				ppVals = append(ppVals, fmt.Sprintf("%s=%s", fieldName, valToString(valType.Interface())))
			} else {
				ppVals = append(ppVals, fmt.Sprintf("%s,%s", fieldName, valToString(valType.Interface())))
			}
		}
		pathParams[parentName] = strings.Join(ppVals, ",")
	default:
		pathParams[parentName] = valToString(objValue.Interface())
	}

	return pathParams
}
