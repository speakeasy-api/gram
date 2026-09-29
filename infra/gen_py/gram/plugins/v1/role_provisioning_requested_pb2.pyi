from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class RoleProvisioningRequested(_message.Message):
    __slots__ = ("organization_id", "role_urn", "plugin_id")
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_URN_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_ID_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    role_urn: str
    plugin_id: str
    def __init__(self, organization_id: _Optional[str] = ..., role_urn: _Optional[str] = ..., plugin_id: _Optional[str] = ...) -> None: ...
