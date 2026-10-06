from gcp.pubsub.v1 import options_pb2 as _options_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class MessageEvent(_message.Message):
    __slots__ = ("id", "type", "organization_id", "project_id", "conversation_id", "message_id", "role", "occurred_at", "message_created_at", "ingestion", "attachment_ids")
    class Type(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        TYPE_UNSPECIFIED: _ClassVar[MessageEvent.Type]
        TYPE_CREATED: _ClassVar[MessageEvent.Type]
        TYPE_ATTRIBUTION_UPDATED: _ClassVar[MessageEvent.Type]
        TYPE_ATTACHMENTS_ADDED: _ClassVar[MessageEvent.Type]
    TYPE_UNSPECIFIED: MessageEvent.Type
    TYPE_CREATED: MessageEvent.Type
    TYPE_ATTRIBUTION_UPDATED: MessageEvent.Type
    TYPE_ATTACHMENTS_ADDED: MessageEvent.Type
    class Role(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
        __slots__ = ()
        ROLE_UNSPECIFIED: _ClassVar[MessageEvent.Role]
        ROLE_SYSTEM: _ClassVar[MessageEvent.Role]
        ROLE_DEVELOPER: _ClassVar[MessageEvent.Role]
        ROLE_USER: _ClassVar[MessageEvent.Role]
        ROLE_ASSISTANT: _ClassVar[MessageEvent.Role]
        ROLE_TOOL: _ClassVar[MessageEvent.Role]
    ROLE_UNSPECIFIED: MessageEvent.Role
    ROLE_SYSTEM: MessageEvent.Role
    ROLE_DEVELOPER: MessageEvent.Role
    ROLE_USER: MessageEvent.Role
    ROLE_ASSISTANT: MessageEvent.Role
    ROLE_TOOL: MessageEvent.Role
    class IngestionContext(_message.Message):
        __slots__ = ("source", "billing_user_id", "assistant_id", "observed_user_email", "provider", "hook_source", "hostname", "account_type", "billing_mode", "replayed")
        SOURCE_FIELD_NUMBER: _ClassVar[int]
        BILLING_USER_ID_FIELD_NUMBER: _ClassVar[int]
        ASSISTANT_ID_FIELD_NUMBER: _ClassVar[int]
        OBSERVED_USER_EMAIL_FIELD_NUMBER: _ClassVar[int]
        PROVIDER_FIELD_NUMBER: _ClassVar[int]
        HOOK_SOURCE_FIELD_NUMBER: _ClassVar[int]
        HOSTNAME_FIELD_NUMBER: _ClassVar[int]
        ACCOUNT_TYPE_FIELD_NUMBER: _ClassVar[int]
        BILLING_MODE_FIELD_NUMBER: _ClassVar[int]
        REPLAYED_FIELD_NUMBER: _ClassVar[int]
        source: str
        billing_user_id: str
        assistant_id: str
        observed_user_email: str
        provider: str
        hook_source: str
        hostname: str
        account_type: str
        billing_mode: str
        replayed: bool
        def __init__(self, source: _Optional[str] = ..., billing_user_id: _Optional[str] = ..., assistant_id: _Optional[str] = ..., observed_user_email: _Optional[str] = ..., provider: _Optional[str] = ..., hook_source: _Optional[str] = ..., hostname: _Optional[str] = ..., account_type: _Optional[str] = ..., billing_mode: _Optional[str] = ..., replayed: _Optional[bool] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ORGANIZATION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    CONVERSATION_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    OCCURRED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    INGESTION_FIELD_NUMBER: _ClassVar[int]
    ATTACHMENT_IDS_FIELD_NUMBER: _ClassVar[int]
    id: str
    type: MessageEvent.Type
    organization_id: str
    project_id: str
    conversation_id: str
    message_id: str
    role: MessageEvent.Role
    occurred_at: str
    message_created_at: str
    ingestion: MessageEvent.IngestionContext
    attachment_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, id: _Optional[str] = ..., type: _Optional[_Union[MessageEvent.Type, str]] = ..., organization_id: _Optional[str] = ..., project_id: _Optional[str] = ..., conversation_id: _Optional[str] = ..., message_id: _Optional[str] = ..., role: _Optional[_Union[MessageEvent.Role, str]] = ..., occurred_at: _Optional[str] = ..., message_created_at: _Optional[str] = ..., ingestion: _Optional[_Union[MessageEvent.IngestionContext, _Mapping]] = ..., attachment_ids: _Optional[_Iterable[str]] = ...) -> None: ...
