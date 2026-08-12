from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class Event(_message.Message):
    __slots__ = ("event_id", "organization_id", "event_type", "payload", "created_at")
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    event_id: str
    organization_id: str
    event_type: str
    payload: bytes
    created_at: str
    def __init__(self, event_id: _Optional[str] = ..., organization_id: _Optional[str] = ..., event_type: _Optional[str] = ..., payload: _Optional[bytes] = ..., created_at: _Optional[str] = ...) -> None: ...
