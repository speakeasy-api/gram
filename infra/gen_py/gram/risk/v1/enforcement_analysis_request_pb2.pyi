from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnforcementAnalysisRequest(_message.Message):
    __slots__ = ("correlation_id", "project_id", "organization_id", "created_at", "deadline", "analyzer", "presidio")
    class PresidioPayload(_message.Message):
        __slots__ = ("content", "entities", "score_threshold")
        CONTENT_FIELD_NUMBER: _ClassVar[int]
        ENTITIES_FIELD_NUMBER: _ClassVar[int]
        SCORE_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
        content: str
        entities: _containers.RepeatedScalarFieldContainer[str]
        score_threshold: float
        def __init__(self, content: _Optional[str] = ..., entities: _Optional[_Iterable[str]] = ..., score_threshold: _Optional[float] = ...) -> None: ...
    CORRELATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    DEADLINE_FIELD_NUMBER: _ClassVar[int]
    ANALYZER_FIELD_NUMBER: _ClassVar[int]
    PRESIDIO_FIELD_NUMBER: _ClassVar[int]
    correlation_id: str
    project_id: str
    organization_id: str
    created_at: str
    deadline: str
    analyzer: str
    presidio: EnforcementAnalysisRequest.PresidioPayload
    def __init__(self, correlation_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., created_at: _Optional[str] = ..., deadline: _Optional[str] = ..., analyzer: _Optional[str] = ..., presidio: _Optional[_Union[EnforcementAnalysisRequest.PresidioPayload, _Mapping]] = ...) -> None: ...
