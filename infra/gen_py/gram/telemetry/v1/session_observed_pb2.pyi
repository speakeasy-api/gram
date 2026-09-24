from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class SessionObserved(_message.Message):
    __slots__ = ("project_id", "message_id", "user_email", "provider", "hook_hostname", "account_type", "billing_mode")
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    HOOK_HOSTNAME_FIELD_NUMBER: _ClassVar[int]
    ACCOUNT_TYPE_FIELD_NUMBER: _ClassVar[int]
    BILLING_MODE_FIELD_NUMBER: _ClassVar[int]
    project_id: str
    message_id: str
    user_email: str
    provider: str
    hook_hostname: str
    account_type: str
    billing_mode: str
    def __init__(self, project_id: _Optional[str] = ..., message_id: _Optional[str] = ..., user_email: _Optional[str] = ..., provider: _Optional[str] = ..., hook_hostname: _Optional[str] = ..., account_type: _Optional[str] = ..., billing_mode: _Optional[str] = ...) -> None: ...
