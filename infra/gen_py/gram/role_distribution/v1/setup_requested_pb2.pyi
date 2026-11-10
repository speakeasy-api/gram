from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class RoleDistributionSetupRequestedV1(_message.Message):
    __slots__ = ("organization_id", "role_urn", "global_role_id", "bootstrap_organization_id", "cursor")
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_URN_FIELD_NUMBER: _ClassVar[int]
    GLOBAL_ROLE_ID_FIELD_NUMBER: _ClassVar[int]
    BOOTSTRAP_ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    role_urn: str
    global_role_id: str
    bootstrap_organization_id: str
    cursor: str
    def __init__(self, organization_id: _Optional[str] = ..., role_urn: _Optional[str] = ..., global_role_id: _Optional[str] = ..., bootstrap_organization_id: _Optional[str] = ..., cursor: _Optional[str] = ...) -> None: ...
