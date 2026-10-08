package otel

import (
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ReservedHookProvenanceKey identifies markers stamped only by trusted canonical
// hook ingestion. Historical source/event/hostname attributes remain producer-owned.
func ReservedHookProvenanceKey(key string) bool {
	for _, marker := range []string{"gram.hook.schema", "gram.hook.canonical_event", "gram.hook.transport", "gram.hook.usage_authority"} {
		if key == marker || strings.HasPrefix(key, marker+".") {
			return true
		}
	}
	return false
}

// StripExternalHookProvenance removes producer-supplied canonical provenance
// from OTLP attributes at every resource, scope and record boundary. It must not
// be called on the trusted in-process canonical hook telemetry path.
func StripExternalHookProvenance(message proto.Message) {
	stripExternalHookProvenanceMessage(message.ProtoReflect())
}

func stripExternalHookProvenanceMessage(message protoreflect.Message) {
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsList() && field.Kind() == protoreflect.MessageKind {
			items := value.List()
			if field.Name() == "attributes" {
				stripHookKeyValues(items, "")
			} else {
				for i := range items.Len() {
					stripExternalHookProvenanceMessage(items.Get(i).Message())
				}
			}
		} else if !field.IsMap() && field.Kind() == protoreflect.MessageKind {
			stripExternalHookProvenanceMessage(value.Message())
		}
		return true
	})
}

func stripHookKeyValues(items protoreflect.List, prefix string) {
	kept := 0
	for i := range items.Len() {
		item := items.Get(i)
		message := item.Message()
		fields := message.Descriptor().Fields()
		key := fields.ByName("key")
		value := fields.ByName("value")
		path := message.Get(key).String()
		if prefix != "" {
			path = prefix + "." + path
		}
		if ReservedHookProvenanceKey(path) {
			continue
		}
		if message.Has(value) {
			stripHookAnyValue(message.Get(value).Message(), path)
		}
		items.Set(kept, item)
		kept++
	}
	items.Truncate(kept)
}

func stripHookAnyValue(message protoreflect.Message, prefix string) {
	fields := message.Descriptor().Fields()
	if field := fields.ByName("kvlist_value"); field != nil && message.Has(field) {
		kvlist := message.Get(field).Message()
		stripHookKeyValues(kvlist.Get(kvlist.Descriptor().Fields().ByName("values")).List(), prefix)
	}
	if field := fields.ByName("array_value"); field != nil && message.Has(field) {
		array := message.Get(field).Message()
		items := array.Get(array.Descriptor().Fields().ByName("values")).List()
		for i := range items.Len() {
			stripHookAnyValue(items.Get(i).Message(), prefix)
		}
	}
}

// SanitizeExternalHookAttribute covers the loose OTLP/JSON values accepted by
// the legacy hooks endpoints, including nested maps and kvlist representations.
func SanitizeExternalHookAttribute(key string, value any) (any, bool) {
	if ReservedHookProvenanceKey(key) {
		return nil, false
	}
	return sanitizeHookJSONValue(value, key), true
}

func sanitizeHookJSONValue(value any, prefix string) any {
	switch object := value.(type) {
	case map[string]any:
		for key, child := range object {
			// OTLP AnyValue wrappers do not add a component to the attribute path.
			if key == "kvlistValue" || key == "arrayValue" {
				object[key] = sanitizeHookJSONValue(child, prefix)
				continue
			}
			if key == "values" {
				if entries, ok := child.([]any); ok {
					kept := make([]any, 0, len(entries))
					for _, entry := range entries {
						if kv, ok := entry.(map[string]any); ok {
							if key, ok := kv["key"].(string); ok {
								path := prefix + "." + key
								if ReservedHookProvenanceKey(path) {
									continue
								}
								kv["value"] = sanitizeHookJSONValue(kv["value"], path)
								kept = append(kept, kv)
								continue
							}
						}
						kept = append(kept, sanitizeHookJSONValue(entry, prefix))
					}
					object[key] = kept
					continue
				}
			}
			path := prefix + "." + key
			if ReservedHookProvenanceKey(path) {
				delete(object, key)
			} else {
				object[key] = sanitizeHookJSONValue(child, path)
			}
		}
	case []any:
		for i, child := range object {
			object[i] = sanitizeHookJSONValue(child, prefix)
		}
	}
	return value
}
