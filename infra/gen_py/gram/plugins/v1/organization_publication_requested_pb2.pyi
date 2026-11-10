from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class OrganizationPublicationRequested(_message.Message):
    __slots__ = ("organization_id", "created_by_user_id", "after_project_id")
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_USER_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    created_by_user_id: str
    after_project_id: str
    def __init__(self, organization_id: _Optional[str] = ..., created_by_user_id: _Optional[str] = ..., after_project_id: _Optional[str] = ...) -> None: ...
