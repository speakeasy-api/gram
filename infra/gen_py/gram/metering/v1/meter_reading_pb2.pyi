from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class MeterReading(_message.Message):
    __slots__ = ("id", "organization_id", "project_id", "meter_id", "operation_id", "unit", "quantity", "occurred_at", "attributes", "corrects_reading_id", "meter_version", "kind", "produced_at", "measurement_method", "adjustment_reason", "source")
    class Kind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        KIND_UNSPECIFIED: _ClassVar[MeterReading.Kind]
        KIND_USAGE: _ClassVar[MeterReading.Kind]
        KIND_ADJUSTMENT: _ClassVar[MeterReading.Kind]
    KIND_UNSPECIFIED: MeterReading.Kind
    KIND_USAGE: MeterReading.Kind
    KIND_ADJUSTMENT: MeterReading.Kind
    class AttributesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    METER_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_ID_FIELD_NUMBER: _ClassVar[int]
    UNIT_FIELD_NUMBER: _ClassVar[int]
    QUANTITY_FIELD_NUMBER: _ClassVar[int]
    OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
    ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    CORRECTS_READING_ID_FIELD_NUMBER: _ClassVar[int]
    METER_VERSION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    PRODUCED_AT_FIELD_NUMBER: _ClassVar[int]
    MEASUREMENT_METHOD_FIELD_NUMBER: _ClassVar[int]
    ADJUSTMENT_REASON_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    id: str
    organization_id: str
    project_id: str
    meter_id: str
    operation_id: str
    unit: str
    quantity: int
    occurred_at: str
    attributes: _containers.ScalarMap[str, str]
    corrects_reading_id: str
    meter_version: int
    kind: MeterReading.Kind
    produced_at: str
    measurement_method: str
    adjustment_reason: str
    source: str
    def __init__(self, id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., meter_id: _Optional[str] = ..., operation_id: _Optional[str] = ..., unit: _Optional[str] = ..., quantity: _Optional[int] = ..., occurred_at: _Optional[str] = ..., attributes: _Optional[_Mapping[str, str]] = ..., corrects_reading_id: _Optional[str] = ..., meter_version: _Optional[int] = ..., kind: _Optional[_Union[MeterReading.Kind, str]] = ..., produced_at: _Optional[str] = ..., measurement_method: _Optional[str] = ..., adjustment_reason: _Optional[str] = ..., source: _Optional[str] = ...) -> None: ...
