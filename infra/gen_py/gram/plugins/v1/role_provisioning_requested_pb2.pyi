from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class RoleProvisioningRequested(_message.Message):
    __slots__ = ("organization_id", "role_urn", "plugin_id", "global_role_urn", "after_organization_id", "after_role_urn", "global_sweep")
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_URN_FIELD_NUMBER: _ClassVar[int]
    PLUGIN_ID_FIELD_NUMBER: _ClassVar[int]
    GLOBAL_ROLE_URN_FIELD_NUMBER: _ClassVar[int]
    AFTER_ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_ROLE_URN_FIELD_NUMBER: _ClassVar[int]
    GLOBAL_SWEEP_FIELD_NUMBER: _ClassVar[int]
    organization_id: str
    role_urn: str
    plugin_id: str
    global_role_urn: str
    after_organization_id: str
    after_role_urn: str
    global_sweep: bool
    def __init__(self, organization_id: _Optional[str] = ..., role_urn: _Optional[str] = ..., plugin_id: _Optional[str] = ..., global_role_urn: _Optional[str] = ..., after_organization_id: _Optional[str] = ..., after_role_urn: _Optional[str] = ..., global_sweep: _Optional[bool] = ...) -> None: ...
