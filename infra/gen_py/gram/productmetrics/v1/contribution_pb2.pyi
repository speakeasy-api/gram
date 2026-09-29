from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Contribution(_message.Message):
    __slots__ = ("organization_id", "project_id", "definition", "event_time_unix_nano", "observed_at_unix_nano", "resource_attributes", "scope_attributes", "point_attributes", "value", "contribution_id")
    class Definition(_message.Message):
        __slots__ = ("scope_name", "scope_version", "name", "description", "unit", "instrument", "temporality")
        SCOPE_NAME_FIELD_NUMBER: _ClassVar[int]
        SCOPE_VERSION_FIELD_NUMBER: _ClassVar[int]
        NAME_FIELD_NUMBER: _ClassVar[int]
        DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
        UNIT_FIELD_NUMBER: _ClassVar[int]
        INSTRUMENT_FIELD_NUMBER: _ClassVar[int]
        TEMPORALITY_FIELD_NUMBER: _ClassVar[int]
        scope_name: str
        scope_version: str
        name: str
        description: str
        unit: str
        instrument: str
        temporality: str
        def __init__(self, scope_name: _Optional[str] = ..., scope_version: _Optional[str] = ..., name: _Optional[str] = ..., description: _Optional[str] = ..., unit: _Optional[str] = ..., instrument: _Optional[str] = ..., temporality: _Optional[str] = ...) -> None: ...
    class Number(_message.Message):
        __slots__ = ("integer", "floating")
        INTEGER_FIELD_NUMBER: _ClassVar[int]
        FLOATING_FIELD_NUMBER: _ClassVar[int]
        integer: int
        floating: float
        def __init__(self, integer: _Optional[int] = ..., floating: _Optional[float] = ...) -> None: ...
    class Attribute(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: Contribution.Value
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[Contribution.Value, _Mapping]] = ...) -> None: ...
    class Value(_message.Message):
        __slots__ = ("text", "boolean", "integer", "floating", "strings", "booleans", "integers", "doubles")
        TEXT_FIELD_NUMBER: _ClassVar[int]
        BOOLEAN_FIELD_NUMBER: _ClassVar[int]
        INTEGER_FIELD_NUMBER: _ClassVar[int]
        FLOATING_FIELD_NUMBER: _ClassVar[int]
        STRINGS_FIELD_NUMBER: _ClassVar[int]
        BOOLEANS_FIELD_NUMBER: _ClassVar[int]
        INTEGERS_FIELD_NUMBER: _ClassVar[int]
        DOUBLES_FIELD_NUMBER: _ClassVar[int]
        text: str
        boolean: bool
        integer: int
        floating: float
        strings: Contribution.StringArray
        booleans: Contribution.BoolArray
        integers: Contribution.IntArray
        doubles: Contribution.DoubleArray
        def __init__(self, text: _Optional[str] = ..., boolean: _Optional[bool] = ..., integer: _Optional[int] = ..., floating: _Optional[float] = ..., strings: _Optional[_Union[Contribution.StringArray, _Mapping]] = ..., booleans: _Optional[_Union[Contribution.BoolArray, _Mapping]] = ..., integers: _Optional[_Union[Contribution.IntArray, _Mapping]] = ..., doubles: _Optional[_Union[Contribution.DoubleArray, _Mapping]] = ...) -> None: ...
    class StringArray(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedScalarFieldContainer[str]
        def __init__(self, values: _Optional[_Iterable[str]] = ...) -> None: ...
    class BoolArray(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedScalarFieldContainer[bool]
        def __init__(self, values: _Optional[_Iterable[bool]] = ...) -> None: ...
    class IntArray(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedScalarFieldContainer[int]
        def __init__(self, values: _Optional[_Iterable[int]] = ...) -> None: ...
    class DoubleArray(_message.Message):
        __slots__ = ("values",)
        VALUES_FIELD_NUMBER: _ClassVar[int]
        values: _containers.RepeatedScalarFieldContainer[float]
        def __init__(self, values: _Optional[_Iterable[float]] = ...) -> None: ...
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    DEFINITION_FIELD_NUMBER: _ClassVar[int]
    EVENT_TIME_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_UNIX_NANO_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    SCOPE_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    POINT_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    CONTRIBUTION_ID_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    project_id: str
    definition: Contribution.Definition
    event_time_unix_nano: int
    observed_at_unix_nano: int
    resource_attributes: _containers.RepeatedCompositeFieldContainer[Contribution.Attribute]
    scope_attributes: _containers.RepeatedCompositeFieldContainer[Contribution.Attribute]
    point_attributes: _containers.RepeatedCompositeFieldContainer[Contribution.Attribute]
    value: Contribution.Number
    contribution_id: str
    def __init__(self, organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., definition: _Optional[_Union[Contribution.Definition, _Mapping]] = ..., event_time_unix_nano: _Optional[int] = ..., observed_at_unix_nano: _Optional[int] = ..., resource_attributes: _Optional[_Iterable[_Union[Contribution.Attribute, _Mapping]]] = ..., scope_attributes: _Optional[_Iterable[_Union[Contribution.Attribute, _Mapping]]] = ..., point_attributes: _Optional[_Iterable[_Union[Contribution.Attribute, _Mapping]]] = ..., value: _Optional[_Union[Contribution.Number, _Mapping]] = ..., contribution_id: _Optional[str] = ...) -> None: ...
