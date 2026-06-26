from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class Message(_message.Message):
    __slots__ = ("id", "type", "created_at", "payload")
    ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    id: str
    type: str
    created_at: str
    payload: bytes
    def __init__(self, id: _Optional[str] = ..., type: _Optional[str] = ..., created_at: _Optional[str] = ..., payload: _Optional[bytes] = ...) -> None: ...
