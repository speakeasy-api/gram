from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnforcementAnalysisResponse(_message.Message):
    __slots__ = ("correlation_id", "created_at", "error", "scan_duration_ms", "analyzer", "presidio")
    class PresidioResult(_message.Message):
        __slots__ = ("detections",)
        DETECTIONS_FIELD_NUMBER: _ClassVar[int]
        detections: _containers.RepeatedCompositeFieldContainer[EnforcementAnalysisResponse.Detection]
        def __init__(self, detections: _Optional[_Iterable[_Union[EnforcementAnalysisResponse.Detection, _Mapping]]] = ...) -> None: ...
    class Detection(_message.Message):
        __slots__ = ("entity_type", "match", "start_pos", "end_pos", "confidence")
        ENTITY_TYPE_FIELD_NUMBER: _ClassVar[int]
        MATCH_FIELD_NUMBER: _ClassVar[int]
        START_POS_FIELD_NUMBER: _ClassVar[int]
        END_POS_FIELD_NUMBER: _ClassVar[int]
        CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
        entity_type: str
        match: str
        start_pos: int
        end_pos: int
        confidence: float
        def __init__(self, entity_type: _Optional[str] = ..., match: _Optional[str] = ..., start_pos: _Optional[int] = ..., end_pos: _Optional[int] = ..., confidence: _Optional[float] = ...) -> None: ...
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    SCAN_DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    ANALYZER_FIELD_NUMBER: _ClassVar[int]
    PRESIDIO_FIELD_NUMBER: _ClassVar[int]
    correlation_id: str
    created_at: str
    error: str
    scan_duration_ms: float
    analyzer: str
    presidio: EnforcementAnalysisResponse.PresidioResult
    def __init__(self, correlation_id: _Optional[str] = ..., created_at: _Optional[str] = ..., error: _Optional[str] = ..., scan_duration_ms: _Optional[float] = ..., analyzer: _Optional[str] = ..., presidio: _Optional[_Union[EnforcementAnalysisResponse.PresidioResult, _Mapping]] = ...) -> None: ...
