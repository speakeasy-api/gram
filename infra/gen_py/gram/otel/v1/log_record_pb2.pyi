from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class LogRecord(_message.Message):
    __slots__ = ("time_unix_nano", "severity_number", "severity_text", "body", "attributes", "dropped_attributes_count", "flags", "trace_id", "span_id", "observed_time_unix_nano", "event_name", "record_id", "resource", "resource_schema_url", "scope", "scope_schema_url", "provenance")
    class SeverityNumber(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        SEVERITY_NUMBER_UNSPECIFIED: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_TRACE: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_TRACE2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_TRACE3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_TRACE4: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_DEBUG: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_DEBUG2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_DEBUG3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_DEBUG4: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_INFO: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_INFO2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_INFO3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_INFO4: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_WARN: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_WARN2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_WARN3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_WARN4: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_ERROR: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_ERROR2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_ERROR3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_ERROR4: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_FATAL: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_FATAL2: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_FATAL3: _ClassVar[LogRecord.SeverityNumber]
        SEVERITY_NUMBER_FATAL4: _ClassVar[LogRecord.SeverityNumber]
    SEVERITY_NUMBER_UNSPECIFIED: LogRecord.SeverityNumber
    SEVERITY_NUMBER_TRACE: LogRecord.SeverityNumber
    SEVERITY_NUMBER_TRACE2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_TRACE3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_TRACE4: LogRecord.SeverityNumber
    SEVERITY_NUMBER_DEBUG: LogRecord.SeverityNumber
    SEVERITY_NUMBER_DEBUG2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_DEBUG3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_DEBUG4: LogRecord.SeverityNumber
    SEVERITY_NUMBER_INFO: LogRecord.SeverityNumber
    SEVERITY_NUMBER_INFO2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_INFO3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_INFO4: LogRecord.SeverityNumber
    SEVERITY_NUMBER_WARN: LogRecord.SeverityNumber
    SEVERITY_NUMBER_WARN2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_WARN3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_WARN4: LogRecord.SeverityNumber
    SEVERITY_NUMBER_ERROR: LogRecord.SeverityNumber
    SEVERITY_NUMBER_ERROR2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_ERROR3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_ERROR4: LogRecord.SeverityNumber
    SEVERITY_NUMBER_FATAL: LogRecord.SeverityNumber
    SEVERITY_NUMBER_FATAL2: LogRecord.SeverityNumber
    SEVERITY_NUMBER_FATAL3: LogRecord.SeverityNumber
    SEVERITY_NUMBER_FATAL4: LogRecord.SeverityNumber
    class Provenance(_message.Message):
        __slots__ = ("source", "organization_id", "project_id")
        SOURCE_FIELD_NUMBER: _ClassVar[int]
        ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
        PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
        source: str
        organization_id: str
        project_id: str
        def __init__(self, source: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ...) -> None: ...
    class Resource(_message.Message):
        __slots__ = ("attributes", "dropped_attributes_count")
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        attributes: _containers.RepeatedCompositeFieldContainer[LogRecord.KeyValue]
        dropped_attributes_count: int
        def __init__(self, attributes: _Optional[_Iterable[_Union[LogRecord.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
    class InstrumentationScope(_message.Message):
        __slots__ = ("name", "version", "attributes", "dropped_attributes_count")
        NAME_FIELD_NUMBER: _ClassVar[int]
        VERSION_FIELD_NUMBER: _ClassVar[int]
        ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
        DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
        name: str
        version: str
        attributes: _containers.RepeatedCompositeFieldContainer[LogRecord.KeyValue]
        dropped_attributes_count: int
        def __init__(self, name: _Optional[str] = ..., version: _Optional[str] = ..., attributes: _Optional[_Iterable[_Union[LogRecord.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ...) -> None: ...
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
        array_value: LogRecord.ArrayValue
        kvlist_value: LogRecord.KeyValueList
        bytes_value: bytes
        def __init__(self, string_value: _Optional[str] = ..., bool_value: _Optional[bool] = ..., int_value: _Optional[int] = ..., double_value: _Optional[float] = ..., array_value: _Optional[_Union[LogRecord.ArrayValue, _Mapping]] = ..., kvlist_value: _Optional[_Union[LogRecord.KeyValueList, _Mapping]] = ..., bytes_value: _Optional[bytes] = ...) -> None: ...
    class ArrayValue(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[LogRecord.AnyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[LogRecord.AnyValue, _Mapping]]] = ...) -> None: ...
    class KeyValueList(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedCompositeFieldContainer[LogRecord.KeyValue]
        def __init__(self, values: _Optional[_Iterable[_Union[LogRecord.KeyValue, _Mapping]]] = ...) -> None: ...
    class KeyValue(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: LogRecord.AnyValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[LogRecord.AnyValue, _Mapping]] = ...) -> None: ...
    TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_NUMBER_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_TEXT_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    DROPPED_ATTRIBUTES_COUNT_FIELD_NUMBER: _ClassVar[int]
    FLAGS_FIELD_NUMBER: _ClassVar[int]
    TRACE_ID_FIELD_NUMBER: _ClassVar[int]
    SPAN_ID_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    EVENT_NAME_FIELD_NUMBER: _ClassVar[int]
    RECORD_ID_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    SCOPE_FIELD_NUMBER: _ClassVar[int]
    SCOPE_SCHEMA_URL_FIELD_NUMBER: _ClassVar[int]
    PROVENANCE_FIELD_NUMBER: _ClassVar[int]
    time_unix_nano: int
    severity_number: LogRecord.SeverityNumber
    severity_text: str
    body: LogRecord.AnyValue
    attributes: _containers.RepeatedCompositeFieldContainer[LogRecord.KeyValue]
    dropped_attributes_count: int
    flags: int
    trace_id: bytes
    span_id: bytes
    observed_time_unix_nano: int
    event_name: str
    record_id: str
    resource: LogRecord.Resource
    resource_schema_url: str
    scope: LogRecord.InstrumentationScope
    scope_schema_url: str
    provenance: LogRecord.Provenance
    def __init__(self, time_unix_nano: _Optional[int] = ..., severity_number: _Optional[_Union[LogRecord.SeverityNumber, str]] = ..., severity_text: _Optional[str] = ..., body: _Optional[_Union[LogRecord.AnyValue, _Mapping]] = ..., attributes: _Optional[_Iterable[_Union[LogRecord.KeyValue, _Mapping]]] = ..., dropped_attributes_count: _Optional[int] = ..., flags: _Optional[int] = ..., trace_id: _Optional[bytes] = ..., span_id: _Optional[bytes] = ..., observed_time_unix_nano: _Optional[int] = ..., event_name: _Optional[str] = ..., record_id: _Optional[str] = ..., resource: _Optional[_Union[LogRecord.Resource, _Mapping]] = ..., resource_schema_url: _Optional[str] = ..., scope: _Optional[_Union[LogRecord.InstrumentationScope, _Mapping]] = ..., scope_schema_url: _Optional[str] = ..., provenance: _Optional[_Union[LogRecord.Provenance, _Mapping]] = ...) -> None: ...
