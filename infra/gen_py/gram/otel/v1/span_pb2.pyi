from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Span(_message.Message):
    __slots__ = ("trace_id", "span_id", "trace_state", "parent_span_id", "name", "kind", "start_time_unix_nano", "end_time_unix_nano", "attributes", "dropped_attributes_count", "events", "dropped_events_count", "links", "dropped_links_count", "status", "flags", "resource", "resource_schema_url", "scope", "scope_schema_url", "provenance")
    class SpanKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        SPAN_KIND_UNSPECIFIED: _ClassVar[Span.SpanKind]
        SPAN_KIND_INTERNAL: _ClassVar[Span.SpanKind]
        SPAN_KIND_SERVER: _ClassVar[Span.SpanKind]
        SPAN_KIND_CLIENT: _ClassVar[Span.SpanKind]
        SPAN_KIND_PRODUCER: _ClassVar[Span.SpanKind]
        SPAN_KIND_CONSUMER: _ClassVar[Span.SpanKind]
    SPAN_KIND_UNSPECIFIED: Span.SpanKind
    SPAN_KIND_INTERNAL: Span.SpanKind
    SPAN_KIND_SERVER: Span.SpanKind
    SPAN_KIND_CLIENT: Span.SpanKind
    SPAN_KIND_PRODUCER: Span.SpanKind
    SPAN_KIND_CONSUMER: Span.SpanKind
    class StatusCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        STATUS_CODE_UNSPECIFIED: _ClassVar[Span.StatusCode]
        STATUS_CODE_OK: _ClassVar[Span.StatusCode]
        STATUS_CODE_ERROR: _ClassVar[Span.StatusCode]
    STATUS_CODE_UNSPECIFIED: Span.StatusCode
    STATUS_CODE_OK: Span.StatusCode
    STATUS_CODE_ERROR: Span.StatusCode
    class Provenance(_message.Message):
        __slots__ = ("source", "organization_id", "project_id", "organization_slug", "project_slug", "api_key_id", "api_key_name")
        SOURCE_FIELD_NUMBER: _ClassVar[int]
        ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
        PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
        ORGANIZATION_SLUG_FIELD_NUMBER: _ClassVar[int]
        PROJECT_SLUG_FIELD_NUMBER: _ClassVar[int]
        API_KEY_ID_FIELD_NUMBER: _ClassVar[int]
        API_KEY_NAME_FIELD_NUMBER: _ClassVar[int]
        source: str
        organization_id: str
        project_id: str
        organization_slug: str
        project_slug: str
        api_key_id: str
        api_key_name: str
        def __init__(self, source: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_slug: _Optional[str] = ..., project_slug: _Optional[str] = ..., api_key_id: _Optional[str] = ..., api_key_name: _Optional[str] = ...) -> None: ...
    class Resource(_message.Message):
        __slots__ = ("attributes", "dropped_attributes_count")
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
        dropped_attributes_count: int
        def __init__(self, attributes: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class InstrumentationScope(_message.Message):
        __slots__ = ("name", "version", "attributes", "dropped_attributes_count")
        NAME_FIELD_NUMBER: _ClassVar[int]
        VERSION_FIELD_NUMBER: _ClassVar[int]
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        name: str
        version: str
        attributes: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
        dropped_attributes_count: int
        def __init__(self, name: _Optional[str] = ..., version: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class Status(_message.Message):
        __slots__ = ("message", "code")
        MESSAGE_FIELD_NUMBER: _ClassVar[int]
        CODE_FIELD_NUMBER: _ClassVar[int]
        message: str
        code: Span.StatusCode
        def __init__(self, message: _Optional[str] = ..., code: _Optional[_Union[Span.StatusCode, str]] = ...) -> None: ...
    class Event(_message.Message):
        __slots__ = ("time_unix_nano", "name", "attributes", "dropped_attributes_count")
        TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
        NAME_FIELD_NUMBER: _ClassVar[int]
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        time_unix_nano: int
        name: str
        attributes: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
        dropped_attributes_count: int
        def __init__(self, time_unix_nano: _Optional[int] = ..., name: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class Link(_message.Message):
        __slots__ = ("trace_id", "span_id", "trace_state", "attributes", "dropped_attributes_count", "flags")
        TRACE_ID_FIELD_NUMBER: _ClassVar[int]
        SPAN_ID_FIELD_NUMBER: _ClassVar[int]
        TRACE_STATE_FIELD_NUMBER: _ClassVar[int]
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        FLAGS_FIELD_NUMBER: _ClassVar[int]
        trace_id: bytes
        span_id: bytes
        trace_state: str
        attributes: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
        dropped_attributes_count: int
        flags: int
        def __init__(self, trace_id: _Optional[bytes] = ..., span_id: _Optional[bytes] = ..., trace_state: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ..., flags: _Optional[int] = ...) -> None: ...
    class AnyValue(_message.Message):
        __slots__ = ("string_value", "bool_value", "int_value", "double_value", "array_value", "kvlist_value", "bytes_value")
        STRING_VALUE_FIELD_NUMBER: _ClassVar[int]
        BOOL_VALUE_FIELD_NUMBER: _ClassVar[int]
        INT_VALUE_FIELD_NUMBER: _ClassVar[int]
        DOUBLE_VALUE_FIELD_NUMBER: _ClassVar[int]
        ARRAY_VALUE_FIELD_NUMBER: _ClassVar[int]
        KVLIST_VALUE_FIELD_NUMBER: _ClassVar[int]
        BYTES_VALUE_FIELD_NUMBER: _ClassVar[int]
        string_value: str
        bool_value: bool
        int_value: int
        double_value: float
        array_value: Span.ArrayValue
        kvlist_value: Span.KeyValueList
        bytes_value: bytes
        def __init__(self, string_value: _Optional[str] = ..., bool_value: _Optional[bool] = ..., int_value: _Optional[int] = ..., double_value: _Optional[float] = ..., array_value: _Optional[_Union[Span.ArrayValue, _Mapping]] = ..., kvlist_value: _Optional[_Union[Span.KeyValueList, _Mapping]] = ..., bytes_value: _Optional[bytes] = ...) -> None: ...
    class ArrayValue(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[Span.AnyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[Span.AnyValue, _Mapping]]] = ...) -> None: ...
    class KeyValueList(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ...) -> None: ...
    class KeyValue(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: Span.AnyValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[Span.AnyValue, _Mapping]] = ...) -> None: ...
    TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    SPAN_ID_FIELD_NUMBER: _ClassVar[int]
    TRACE_STATE_FIELD_NUMBER: _ClassVar[int]
    PARENT_SPAN_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    START_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    END_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    DROPPED_EVENTS_COUNT_FIELD_NUMBER: _ClassVar[int]
    LINKS_FIELD_NUMBER: _ClassVar[int]
    DROPPED_LINKS_COUNT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    FLAGS_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    SCOPE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    PROVENANCE_FIELD_NUMBER: _ClassVar[int]
    trace_id: bytes
    span_id: bytes
    trace_state: str
    parent_span_id: bytes
    name: str
    kind: Span.SpanKind
    start_time_unix_nano: int
    end_time_unix_nano: int
    attributes: _containers.RepeatedCompositeFieldContainer[Span.KeyValue]
    dropped_attributes_count: int
    events: _containers.RepeatedCompositeFieldContainer[Span.Event]
    dropped_events_count: int
    links: _containers.RepeatedCompositeFieldContainer[Span.Link]
    dropped_links_count: int
    status: Span.Status
    flags: int
    resource: Span.Resource
    resource_schema_url: str
    scope: Span.InstrumentationScope
    scope_schema_url: str
    provenance: Span.Provenance
    def __init__(self, trace_id: _Optional[bytes] = ..., span_id: _Optional[bytes] = ..., trace_state: _Optional[str] = ..., parent_span_id: _Optional[bytes] = ..., name: _Optional[str] = ..., kind: _Optional[_Union[Span.SpanKind, str]] = ..., start_time_unix_nano: _Optional[int] = ..., end_time_unix_nano: _Optional[int] = ..., attributes: _Optional[_Iterable[_Union[Span.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ..., events: _Optional[_Iterable[_Union[Span.Event, _Mapping]]] = ..., dropped_events_count: _Optional[int] = ..., links: _Optional[_Iterable[_Union[Span.Link, _Mapping]]] = ..., dropped_links_count: _Optional[int] = ..., status: _Optional[_Union[Span.Status, _Mapping]] = ..., flags: _Optional[int] = ..., resource: _Optional[_Union[Span.Resource, _Mapping]] = ..., resource_schema_url: _Optional[str] = ..., scope: _Optional[_Union[Span.InstrumentationScope, _Mapping]] = ..., scope_schema_url: _Optional[str] = ..., provenance: _Optional[_Union[Span.Provenance, _Mapping]] = ...) -> None: ...
