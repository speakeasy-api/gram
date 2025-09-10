package serialization

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func valToString(val interface{}) string {
	switch v := val.(type) {
	case time.Time:
		return v.Format(time.RFC3339Nano)

	// Floats
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)

	// Signed ints
	case int, int8, int16, int32:
		return strconv.FormatInt(reflect.ValueOf(v).Int(), 10)
	case int64:
		return strconv.FormatInt(v, 10)

	// Unsigned ints
	case uint, uint8, uint16, uint32:
		return strconv.FormatUint(reflect.ValueOf(v).Uint(), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

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

	if typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Interface {
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
