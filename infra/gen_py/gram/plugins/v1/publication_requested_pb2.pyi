from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class PublicationRequested(_message.Message):
    __slots__ = ("organization_id", "project_id", "created_by_user_id", "include_paused_plugins")
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_USER_ID_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_PAUSED_PLUGINS_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    project_id: str
    created_by_user_id: str
    include_paused_plugins: bool
    def __init__(self, organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., created_by_user_id: _Optional[str] = ..., include_paused_plugins: _Optional[bool] = ...) -> None: ...
