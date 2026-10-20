from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class ReviewChanged(_message.Message):
    __slots__ = ("event_id", "organization_id", "project_id", "request_id", "decision_id", "event_name", "occurred_at", "recipient_user_ids")
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    DECISION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_NAME_FIELD_NUMBER: _ClassVar[int]
    OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
    RECIPIENT_USER_IDS_FIELD_NUMBER: _ClassVar[int]
    event_id: str
    organization_id: str
    project_id: str
    request_id: str
    decision_id: str
    event_name: str
    occurred_at: str
    recipient_user_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, event_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., request_id: _Optional[str] = ..., decision_id: _Optional[str] = ..., event_name: _Optional[str] = ..., occurred_at: _Optional[str] = ..., recipient_user_ids: _Optional[_Iterable[str]] = ...) -> None: ...
