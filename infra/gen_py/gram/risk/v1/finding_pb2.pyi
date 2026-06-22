from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class Finding(_message.Message):
    __slots__ = ("id", "request_id", "chat_message_id", "project_id", "organization_id", "risk_policy_id", "risk_policy_version", "created_at", "rule_id", "description", "match", "start_pos", "end_pos", "tags", "source", "confidence", "dead_letter_reason")
    ID_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_ID_FIELD_NUMBER: _ClassVar[int]
    RISK_POLICY_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    RULE_ID_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    MATCH_FIELD_NUMBER: _ClassVar[int]
    START_POS_FIELD_NUMBER: _ClassVar[int]
    END_POS_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    CONFIDENCE_FIELD_NUMBER: _ClassVar[int]
    DEAD_LETTER_REASON_FIELD_NUMBER: _ClassVar[int]
    id: str
    request_id: str
    chat_message_id: str
    project_id: str
    organization_id: str
    risk_policy_id: str
    risk_policy_version: int
    created_at: str
    rule_id: str
    description: str
    match: str
    start_pos: int
    end_pos: int
    tags: _containers.RepeatedScalarFieldContainer[str]
    source: str
    confidence: float
    dead_letter_reason: str
    def __init__(self, id: _Optional[str] = ..., request_id: _Optional[str] = ..., chat_message_id: _Optional[str] = ..., project_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., risk_policy_id: _Optional[str] = ..., risk_policy_version: _Optional[int] = ..., created_at: _Optional[str] = ..., rule_id: _Optional[str] = ..., description: _Optional[str] = ..., match: _Optional[str] = ..., start_pos: _Optional[int] = ..., end_pos: _Optional[int] = ..., tags: _Optional[_Iterable[str]] = ..., source: _Optional[str] = ..., confidence: _Optional[float] = ..., dead_letter_reason: _Optional[str] = ...) -> None: ...
