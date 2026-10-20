from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class Delivery(_message.Message):
    __slots__ = ("id", "organization_id", "project_id")
    ID_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    organization_id: str
    project_id: str
    def __init__(self, id: _Optional[str] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ...) -> None: ...
