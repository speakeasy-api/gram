package serialization

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func valToString(val any) string {
	switch v := val.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano)

	case json.Number:
		return v.String()

	default:
		return fmt.Sprintf("%v", v)
	}
}

func isNil(typ reflect.Type, val reflect.Value) bool {
	// `reflect.TypeOf(nil) == nil` so calling typ.Kind() will cause a nil pointer
	// dereference panic. Catch it and return early.
	// https://github.com/golang/go/issues/51649
	// https://github.com/golang/go/issues/54208
	if typ == nil {
		return true
	}

	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Interface {
		return val.IsNil()
	}

	return false
}

func parseDelimitedArray(explode bool, objValue reflect.Value, delimiter string) []string {
	values := []string{}
	items := []string{}

	for i := range objValue.Len() {
		if explode {
			values = append(values, valToString(objValue.Index(i).Interface()))
		} else {
			items = append(items, valToString(objValue.Index(i).Interface()))
		}
	}

	if len(items) > 0 {
		values = append(values, strings.Join(items, delimiter))
	}

	return values
}
